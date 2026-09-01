package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const questionRequestTimeout = 170 * time.Second

type questionService struct {
	db              *sql.DB
	llm             llmClient
	moderate        moderationClient
	thumbnailMirror thumbnailMirror
}

type llmClient interface {
	Answer(context.Context, string) (string, string, error)
}
type moderationClient interface {
	Check(context.Context, string) (bool, error)
}

type mockLLM struct{}

func (mockLLM) Answer(_ context.Context, content string) (string, string, error) {
	return "## 回答\n\n这是开发环境的 Mock 回答。你的问题是：" + content, "mock", nil
}

func newQuestionService(db *sql.DB) (*questionService, error) {
	provider := getenv("LLM_PROVIDER", "mock")
	var llm llmClient
	if provider == "mock" {
		llm = mockLLM{}
	} else if provider == "openai_compatible" {
		client, err := newOpenAIClient()
		if err != nil {
			return nil, err
		}
		llm = client
	} else {
		return nil, errors.New("LLM_PROVIDER must be mock or openai_compatible")
	}
	moderation, err := newModerationClientFromEnv()
	if err != nil {
		return nil, err
	}
	mirror, err := newThumbnailMirrorFromEnv()
	if err != nil {
		return nil, err
	}
	return &questionService{db: db, llm: llm, moderate: moderation, thumbnailMirror: mirror}, nil
}

func (s *questionService) registerRoutes(api *gin.RouterGroup, auth *authService) {
	questions := api.Group("/questions", auth.requireAccessToken())
	questions.POST("", s.create)
	questions.GET("", s.list)
	questions.GET("/:id", s.detail)
}

func currentUserID(c *gin.Context) (int64, bool) {
	value, ok := c.Get(userIDContextKey)
	userID, valid := value.(int64)
	return userID, ok && valid && userID > 0
}

func (s *questionService) create(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
		return
	}
	if s.db == nil {
		writeError(c, http.StatusServiceUnavailable, "QUESTION_SAVE_FAILED", "问题服务暂时不可用，请稍后再试")
		return
	}
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_CONTENT", "问题不能为空")
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		writeError(c, http.StatusBadRequest, "INVALID_CONTENT", "问题不能为空")
		return
	}
	if len([]rune(req.Content)) > 2000 {
		writeError(c, http.StatusBadRequest, "CONTENT_TOO_LONG", "问题不能超过 2000 个字符")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), questionRequestTimeout)
	defer cancel()
	allowed, err := s.moderate.Check(ctx, req.Content)
	if err != nil {
		writeError(c, http.StatusBadGateway, "MODERATION_UNAVAILABLE", "内容审核服务暂时不可用，请稍后再试")
		return
	}
	if !allowed {
		_, saveErr := s.db.ExecContext(ctx, "INSERT INTO questions(user_id, content, status, rejection_reason) VALUES (?, ?, 'rejected', 'policy')", userID, req.Content)
		if saveErr != nil {
			writeError(c, http.StatusInternalServerError, "QUESTION_SAVE_FAILED", "问题保存失败，请稍后重试")
			return
		}
		writeError(c, http.StatusBadRequest, "CONTENT_REJECTED", "这个问题未通过内容审核，无法回答。")
		return
	}
	result, err := s.db.ExecContext(ctx, "INSERT INTO questions(user_id, content, status) VALUES (?, ?, 'pending')", userID, req.Content)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "QUESTION_SAVE_FAILED", "问题保存失败，请稍后重试")
		return
	}
	questionID, err := result.LastInsertId()
	if err != nil {
		writeError(c, http.StatusInternalServerError, "QUESTION_SAVE_FAILED", "问题保存失败，请稍后重试")
		return
	}
	started := time.Now()
	answer, model, err := s.llm.Answer(ctx, req.Content)
	if err != nil {
		s.markQuestionStatus(questionID, userID, "failed", nil)
		writeError(c, http.StatusBadGateway, "LLM_UNAVAILABLE", "这个问题没有编译成功。网络或模型暂时不可用，请重试。")
		return
	}
	answerAllowed, err := s.moderate.Check(ctx, answer)
	if err != nil {
		reason := "moderation_unavailable"
		s.markQuestionStatus(questionID, userID, "failed", &reason)
		writeError(c, http.StatusBadGateway, "MODERATION_UNAVAILABLE", "内容审核服务暂时不可用，请稍后再试")
		return
	}
	if !answerAllowed {
		reason := "policy"
		s.markQuestionStatus(questionID, userID, "rejected", &reason)
		writeError(c, http.StatusBadRequest, "CONTENT_REJECTED", "这个问题未通过内容审核，无法回答。")
		return
	}
	linkCards := enrichLinkCards(ctx, buildLinkCards(answer))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.markQuestionStatus(questionID, userID, "failed", nil)
		writeError(c, http.StatusInternalServerError, "QUESTION_SAVE_FAILED", "问题保存失败，请稍后重试")
		return
	}
	var answerID int64
	thumbnailTasks := make([]thumbnailTask, 0, len(linkCards))
	if answerResult, insertErr := tx.ExecContext(ctx, "INSERT INTO answers(question_id, content, model, duration_ms) VALUES (?, ?, ?, ?)", questionID, answer, model, time.Since(started).Milliseconds()); insertErr != nil {
		err = insertErr
	} else if answerID, err = answerResult.LastInsertId(); err == nil {
		for _, card := range linkCards {
			var cardResult sql.Result
			cardResult, err = tx.ExecContext(ctx, "INSERT INTO link_cards(answer_id, url, title, description, image_url, media_type, position, site_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", answerID, card.URL, card.Title, card.Description, card.ImageURL, card.MediaType, card.Position, card.SiteName)
			if err != nil {
				break
			}
			if s.thumbnailMirror != nil && card.ImageURL != nil {
				var cardID int64
				if cardID, err = cardResult.LastInsertId(); err != nil {
					break
				}
				thumbnailTasks = append(thumbnailTasks, thumbnailTask{cardID: cardID, sourceURL: *card.ImageURL})
			}
		}
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE questions SET status='answered' WHERE id=? AND user_id=?", questionID, userID)
	}
	if err != nil {
		_ = tx.Rollback()
		s.markQuestionStatus(questionID, userID, "failed", nil)
		writeError(c, http.StatusInternalServerError, "QUESTION_SAVE_FAILED", "问题保存失败，请稍后重试")
		return
	}
	if err = tx.Commit(); err != nil {
		s.markQuestionStatus(questionID, userID, "failed", nil)
		writeError(c, http.StatusInternalServerError, "QUESTION_SAVE_FAILED", "问题保存失败，请稍后重试")
		return
	}
	s.scheduleThumbnailMirrors(thumbnailTasks)
	q, err := s.getQuestion(ctx, userID, questionID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "QUESTION_SAVE_FAILED", "问题保存失败，请稍后重试")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": q, "request_id": c.GetString("request_id")})
}

type thumbnailTask struct {
	cardID    int64
	sourceURL string
}

func (s *questionService) scheduleThumbnailMirrors(tasks []thumbnailTask) {
	if s.thumbnailMirror == nil || len(tasks) == 0 {
		return
	}
	for _, task := range tasks {
		go s.mirrorThumbnail(task)
	}
}

func (s *questionService) mirrorThumbnail(task thumbnailTask) {
	mirrorCtx, mirrorCancel := context.WithTimeout(context.Background(), 30*time.Second)
	mirroredURL, err := s.thumbnailMirror.Mirror(mirrorCtx, task.sourceURL)
	mirrorCancel()
	if err != nil {
		slog.Warn("thumbnail mirror failed", "card_id", task.cardID)
		return
	}
	if mirroredURL == "" || len([]rune(mirroredURL)) > maxLinkURLRunes {
		slog.Warn("thumbnail mirror returned invalid URL", "card_id", task.cardID)
		return
	}
	updateCtx, updateCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer updateCancel()
	if _, err = s.db.ExecContext(updateCtx, "UPDATE link_cards SET image_url=? WHERE id=? AND image_url=?", mirroredURL, task.cardID, task.sourceURL); err != nil {
		slog.Warn("thumbnail URL update failed", "card_id", task.cardID)
	}
}

func (s *questionService) markQuestionStatus(questionID, userID int64, status string, reason *string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(ctx, "UPDATE questions SET status=?, rejection_reason=? WHERE id=? AND user_id=?", status, reason, questionID, userID)
}

func (s *questionService) list(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
		return
	}
	page, size, err := pageParams(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_PAGINATION", "分页参数不合法")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	var total int
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM questions WHERE user_id=?", userID).Scan(&total); err != nil {
		writeError(c, http.StatusInternalServerError, "QUESTION_QUERY_FAILED", "问题查询失败，请稍后再试")
		return
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, content, status, rejection_reason, created_at FROM questions WHERE user_id=? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?", userID, size, (page-1)*size)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "QUESTION_QUERY_FAILED", "问题查询失败，请稍后再试")
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id int64
		var content, status string
		var reason sql.NullString
		var created time.Time
		if err = rows.Scan(&id, &content, &status, &reason, &created); err != nil {
			writeError(c, http.StatusInternalServerError, "QUESTION_QUERY_FAILED", "问题查询失败，请稍后再试")
			return
		}
		content = visibleQuestionContent(content, status)
		items = append(items, gin.H{"id": id, "content": content, "status": status, "rejection_reason": nullable(reason), "created_at": created.Format(time.RFC3339Nano)})
	}
	if err = rows.Err(); err != nil {
		writeError(c, http.StatusInternalServerError, "QUESTION_QUERY_FAILED", "问题查询失败，请稍后再试")
		return
	}
	pages := (total + size - 1) / size
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"items": items, "pagination": gin.H{"page": page, "size": size, "total": total, "total_pages": pages, "has_next": page < pages}}, "request_id": c.GetString("request_id")})
}

func (s *questionService) detail(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusNotFound, "QUESTION_NOT_FOUND", "问题不存在")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	q, err := s.getQuestion(ctx, userID, id)
	if err == sql.ErrNoRows {
		writeError(c, http.StatusNotFound, "QUESTION_NOT_FOUND", "问题不存在")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "QUESTION_QUERY_FAILED", "问题查询失败，请稍后再试")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": q, "request_id": c.GetString("request_id")})
}

func (s *questionService) getQuestion(ctx context.Context, userID, id int64) (gin.H, error) {
	var content, status string
	var reason sql.NullString
	var created time.Time
	err := s.db.QueryRowContext(ctx, "SELECT content, status, rejection_reason, created_at FROM questions WHERE id=? AND user_id=?", id, userID).Scan(&content, &status, &reason, &created)
	if err != nil {
		return nil, err
	}
	content = visibleQuestionContent(content, status)
	q := gin.H{"id": id, "content": content, "status": status, "rejection_reason": nullable(reason), "created_at": created.Format(time.RFC3339Nano), "answer": nil}
	if status == "answered" {
		var answer, model string
		var tokens, duration sql.NullInt64
		err = s.db.QueryRowContext(ctx, "SELECT content, model, tokens_used, duration_ms FROM answers WHERE question_id=?", id).Scan(&answer, &model, &tokens, &duration)
		if err != nil {
			return nil, err
		}
		cards, cardErr := s.db.QueryContext(ctx, "SELECT id, url, title, description, image_url, media_type, position, site_name FROM link_cards WHERE answer_id=(SELECT id FROM answers WHERE question_id=?) ORDER BY position ASC", id)
		if cardErr != nil {
			return nil, cardErr
		}
		defer cards.Close()
		linkCards := make([]gin.H, 0)
		for cards.Next() {
			var cardID int64
			var url, mediaType string
			var title, description, imageURL, siteName sql.NullString
			var position int
			if err = cards.Scan(&cardID, &url, &title, &description, &imageURL, &mediaType, &position, &siteName); err != nil {
				return nil, err
			}
			linkCards = append(linkCards, gin.H{"id": cardID, "url": url, "title": nullable(title), "description": nullable(description), "image_url": nullable(imageURL), "media_type": mediaType, "position": position, "site_name": nullable(siteName)})
		}
		if err = cards.Err(); err != nil {
			return nil, err
		}
		q["answer"] = gin.H{"content": answer, "model": model, "tokens_used": nullable(tokens), "duration_ms": nullable(duration), "link_cards": linkCards}
	}
	return q, nil
}

func nullable(value interface{}) interface{} {
	switch v := value.(type) {
	case sql.NullString:
		if v.Valid {
			return v.String
		}
	case sql.NullInt64:
		if v.Valid {
			return v.Int64
		}
	}
	return nil
}

func visibleQuestionContent(content, status string) string {
	if status == "rejected" {
		return "……"
	}
	return content
}

func pageParams(c *gin.Context) (int, int, error) {
	page, size := 1, 20
	var err error
	if c.Query("page") != "" {
		page, err = strconv.Atoi(c.Query("page"))
		if err != nil || page < 1 {
			return 0, 0, errors.New("page must be at least 1")
		}
	}
	if c.Query("size") != "" {
		size, err = strconv.Atoi(c.Query("size"))
		if err != nil || size < 1 || size > 20 {
			return 0, 0, errors.New("size must be between 1 and 20")
		}
	}
	return page, size, nil
}
