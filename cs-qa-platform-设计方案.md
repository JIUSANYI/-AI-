# 计算机知识 AI 问答平台 · 设计方案

| 文档属性 | 内容 |
|---|---|
| 项目名称 | cs-qa-platform |
| 文档版本 | v1.1 |
| 编写日期 | 2026-08-30（2026-08-31 修订） |
| 文档状态 | 已确认，实施中 |
| 关联文档 | 《需求分析文档》《技术栈文档》《接口文档》 |

---

## 1. 需求回顾

| 项 | 内容 |
|---|---|
| 目标用户 | 大学计算机在校生、在职计算机从业者、计算机爱好者 |
| 核心功能 | ① 向 AI 提问计算机相关问题并获得回答；② AI 回答中自动附带相关资源链接，以卡片形式展示视频和图片 |
| 技术栈 | 后端 Go（Gin）+ 前端 Next.js + MySQL + Redis |
| 部署 | 腾讯云/阿里云 2核2G 单机；静态资源存七牛云 |
| 认证 | 手机验证码登录（腾讯云 SMS），一期不划分角色 |
| 规模 | 一期约 10 用户，不考虑并发与实时性 |
| 已确认决策 | AI 接第三方大模型 API；内容审核需要（敏感词 + 腾讯云天御） |

---

## 2. 总体架构

```
用户浏览器 ──HTTPS──> Nginx 反向代理
                        ├──> Next.js 前端（Node standalone，约 250MB）
                        └──> Go 后端 API（Gin 单二进制，约 50MB）
                                  ├──> MySQL 8（约 500MB）
                                  ├──> Redis 7（验证码/限流/吊销名单，约 50MB）
                                  └──> 外部服务
                                        ├──> 大模型 API（OpenAI 兼容协议）
                                        ├──> 腾讯云 SMS（验证码）
                                        ├──> 腾讯云天御（文本审核）
                                        └──> 七牛云（缩略图等静态资源）
```

### 2.1 关键架构决策

### 2.1.1 前后端职责边界

本阶段采用前后端协同、职责分离策略：

- 本开发 Agent 同时负责 `backend/`、`frontend/` 的工程实现、联调、测试和部署配置。
- UI 协作模型负责视觉设计、页面布局、交互细节和视觉验收；本 Agent 负责工程实现与接口契约。
- 前端与后端通过版本化的 REST/JSON 接口协作，接口契约以《接口文档》为准。
- Docker Compose 中前端和后端仍可同机运行，分别构建、联调和发布；前端变更必须经过构建与浏览器冒烟验证。
- 测试环境和正式环境均遵守该边界，后端只更新对应环境的 `backend` 服务。

| 决策项     | 方案                                                                           | 理由                         |
| ------- | ---------------------------------------------------------------------------- | -------------------------- |
| 后端语言/框架 | Go 1.22+ / Gin                                                               | 单文件二进制、内存占用极低，适配 2核2G      |
| 项目结构    | 按功能组织 + 三层（handler → service → repository）                                   | 控制器零业务逻辑，二期加角色/权限演进成本低     |
| API 风格  | REST + JSON，统一前缀 `/api/v1`                                                   | 请求-响应式问答场景，无需 GraphQL/tRPC |
| 数据库变更   | 版本化 SQL 迁移（启动时自动执行，记录于 schema_migrations 表）                                  | 可回滚、可追溯                    |
| 认证      | JWT 短时访问令牌（30 分钟，仅存内存）+ 刷新令牌（30 天，httpOnly Cookie 下发，SHA-256 哈希后存 MySQL，可吊销） | 令牌不落 localStorage，符合安全基线   |
| SMS     | 腾讯云 SMS 官方 SDK；开发期 Mock 模式（验证码仅打日志）                                          | 不阻塞本地开发，密钥只进环境变量           |
| AI 接入   | OpenAI 兼容协议 HTTP 客户端，Base URL/Key/Model 全部环境变量化                              | 可随时切换混元/DeepSeek/通义等厂商     |
| 链接卡片    | AI 输出中的 URL → 后端抓取 og/meta → 标题+缩略图卡片，缩略图转存七牛云                               | 防盗链失效、页面加载快                |
| 内容审核    | 双层：本地敏感词（第一层，零成本）+ 腾讯云天御文本审核（第二层）                                            | 本地过滤降低 API 成本，云端兜底合规       |
| 实时能力    | 一期不引入                                                                        | 问答为同步请求-响应                 |
| 部署      | Docker Compose 单机 + 2G swap                                                  | 四个服务内存预算约 900MB，留余量        |
|         |                                                                              |                            |

### 2.2 后端目录结构

```
backend/
├── main.go                      # 启动入口：加载配置→连接依赖→执行迁移→启动HTTP→优雅停机
├── .env.example                 # 仅占位值，不提交真实密钥
├── migrations/                  # 版本化 SQL 迁移（go:embed 打进二进制）
│   ├── 0001_init.sql
│   └── ...
└── internal/
    ├── config/                  # 环境变量集中加载 + 启动时校验，快速失败
    ├── errs/                   # 类型化错误体系（Code/HTTPStatus/Message）
    ├── middleware/              # recover、请求ID、访问日志、CORS、JWT 鉴权、限流
    ├── platform/                # db.go / redis.go / migrate.go / httpclient.go
    └── modules/
        ├── auth/                # handler.go / service.go / repo.go / sms.go / token.go
        ├── questions/           # handler.go / service.go / repo.go
        ├── ai/                  # LLM 客户端（OpenAI 兼容）
        ├── moderation/          # 敏感词 + 天御审核
        └── mediacard/           # 链接抓取（og/meta）+ 七牛转存
```

### 2.3 前端目录结构

```
frontend/
├── src/
│   ├── app/
│   │   ├── layout.tsx           # 全局布局 + AuthProvider
│   │   ├── page.tsx             # 提问页（核心页面）
│   │   ├── login/page.tsx       # 手机验证码登录
│   │   ├── history/page.tsx     # 历史提问列表（分页）
│   │   └── questions/[id]/page.tsx  # 问答详情
│   ├── components/              # LinkCard / MarkdownView / QuestionForm / ...
│   ├── lib/
│   │   ├── api.ts               # 类型化 fetch 封装：自动附加令牌、401 刷新重试（1次）
│   │   └── types.ts             # 后端响应类型定义
│   └── context/AuthContext.tsx  # 访问令牌存内存，刷新恢复会话
├── next.config.js               # standalone 输出
└── Dockerfile
```

---

## 3. 数据库设计

共 5 张运行时表（InnoDB / utf8mb4）：

| 表 | 说明 | 关键字段 |
|---|---|---|
| users | 用户 | id, phone(唯一), nickname, status, created_at |
| refresh_tokens | 刷新令牌 | user_id, token_hash(SHA-256, 唯一), expires_at, revoked_at |
| questions | 提问 | user_id, content, status(pending/answered/rejected/failed) |
| answers | 回答 | question_id(唯一), content(Markdown), model, tokens_used, duration_ms |
| link_cards | 链接卡片（可空，按需生成） | answer_id, url, title, description, image_url, media_type, position |

索引：`questions(user_id, created_at)`、`link_cards(answer_id, position)`、`refresh_tokens(user_id, expires_at)`。

> Redis 用途：保存验证码哈希（10 分钟 TTL）、验证码发送频率限制（60s/次、5条/天）、提问限流（同一用户每小时 5 次、同一客户端 IP 每小时 20 次）和刷新令牌吊销状态。验证码不落 MySQL；历史迁移 `0002_auth.sql` 创建过 `sms_codes`，由 `0005_drop_sms_codes.sql` 清理。

---

## 4. API 契约（前缀 /api/v1）

| 方法   | 路径                   | 说明                       | 鉴权     |
| ---- | -------------------- | ------------------------ | ------ |
| POST | /auth/sms-code       | 发送验证码                    | 否      |
| POST | /auth/login          | 验证码登录/注册（自动注册）           | 否      |
| POST | /auth/refresh        | 用 httpOnly Cookie 刷新访问令牌 | Cookie |
| POST | /auth/logout         | 吊销刷新令牌并清 Cookie          | Cookie |
| POST | /questions           | 提问（同步返回完整回答+卡片）          | 是      |
| GET  | /questions?page&size | 我的历史提问（分页）               | 是      |
| GET  | /questions/:id       | 问答详情（含卡片）                | 是      |
| GET  | /health /ready       | 健康检查/就绪检查                | 否      |

**统一响应结构**

```jsonc
// 成功
{ "data": { ... }, "request_id": "a1b2c3" }
// 失败
{ "error": { "code": "INVALID_SMS_CODE", "message": "验证码错误或已过期" }, "request_id": "a1b2c3" }
```

**提问请求/响应示例**

```jsonc
// POST /questions
{ "content": "什么是进程和线程的区别？" }

// 响应 data：
{
  "id": 1,
  "content": "什么是进程和线程的区别？",
  "status": "answered",
  "created_at": "2026-08-30T19:00:00+08:00",
  "answer": {
    "content": "## 进程与线程\n...（Markdown 正文）",
    "model": "deepseek-chat",
    "link_cards": [
      { "url": "https://www.bilibili.com/video/BVxxx", "title": "进程线程详解",
        "image_url": "https://cdn.qiniu.xxx/thumb/xxx.jpg", "media_type": "video" }
    ]
    // link_cards 按需生成：AI 判断无必要附链接时为空数组 []
  }
}
```

---

## 5. 核心流程

### 5.1 登录流程

```
输入手机号 → POST /auth/sms-code（Redis 限流校验）
  → 腾讯云 SMS 下发（Mock 模式：日志输出）
→ 输入验证码 → POST /auth/login
  → 原子校验并删除 Redis 中的验证码哈希（未过期/匹配）→ 无则注册用户
  → 签发 accessToken（30min，JSON 返回，前端仅存内存）
  → 签发 refreshToken（30天，SHA-256 入库，httpOnly Cookie 下发）
→ 前端 401 时自动 POST /auth/refresh（轮换刷新令牌）重试一次
```

### 5.2 提问答疑流程（同步，单请求）

```
POST /questions
  1. 校验长度 1~2000 字
  2. Redis 原子限流：同一用户每小时最多 5 次、同一客户端 IP 每小时最多 20 次（超限返回 429）
  3. 审核层1：本地敏感词过滤（拒绝 → status=rejected）
  4. 审核层2：天御文本审核（拒绝 → status=rejected）
  5. 插入 questions(status=pending)
  6. 调用 LLM（超时 120s），system prompt 引导：
     - 以 Markdown 回答，结构清晰（定义/对比/示例/小结）
     - **仅当补充资源对理解确有帮助时**，才在末尾输出「参考资源」小节，
       列 0~4 个高质量真实链接（官方文档、权威教程、B站/YouTube 视频）；
       概念解释类简单问题可以不带任何链接
  7. 若回答中含 URL（最多 4 个）→ 并发抓取 og:title/og:description/og:image/oEmbed
     → 生成 link_cards（可为空）；缩略图异步转存七牛云（未配置则保留原图 URL）
  8. 事务写入 answers + link_cards，questions.status=answered
  9. 返回完整结果
```

**media_type 判定规则**：B站/YouTube 域名 → `video`（前端 iframe 嵌入播放）；og:image 醒目或图片直链（.jpg/.png/.webp）→ `image`；其余 → `link`（标题+缩略图卡片）。

**降级策略**：LLM 超时/失败 → status=failed，返回可重试错误；前端仅对幂等 GET 在 5xx 时自动重试，POST 提问/重试不自动重放以避免重复调用模型；单链接抓取失败 → 仅保留 URL 纯链接卡片，不阻塞整体回答。

---

## 6. 前端设计

| 页面 | 内容 |
|---|---|
| /login | 手机号 + 验证码输入，60 秒倒计时重发；错误码映射为友好文案 |
| /（提问页） | 大输入框 + 提交；提交后展示加载动画（预计 10~30s）；回答区 Markdown 渲染（标题/代码高亮/表格），底部「参考资源」渲染为卡片：视频卡片内嵌播放、图片卡片直接展示、普通链接卡片带缩略图与标题 |
| /history | 我的提问列表（状态标签：已回答/审核拒绝/失败），分页 |
| /questions/[id] | 完整问答详情，同提问页的渲染组件 |

**API 客户端规范**：Base URL 走 `NEXT_PUBLIC_API_BASE_URL`；4xx 不重试、幂等 GET 的 5xx 自动重试最多 3 次，POST 不自动重放；网络失败显示离线提示；提问请求前端超时为 135 秒并提示用户查看历史记录；错误统一映射为中文文案；401 触发一次令牌刷新后重放原请求。

**令牌策略**：访问令牌仅存内存（AuthContext）；刷新令牌在 httpOnly Cookie 中，页面刷新时静默调 /auth/refresh 恢复会话。

---

## 7. 工程规范

- **配置**：全部来自环境变量，启动时集中校验并快速失败；`SMS_PROVIDER=mock` 时不需要腾讯密钥即可本地跑通。
- **错误**：类型化错误类 + 全局中间件，客户端只见 `code/message/request_id`，绝不返回堆栈。
- **日志**：结构化 JSON 日志，贯穿请求 ID；不记录手机号明文（脱敏 138****1234）、令牌、验证码。
- **安全**：CORS 显式白名单（生产禁通配符）；安全头（X-Content-Type-Options 等）；验证码接口 Redis 限流防刷；MySQL/Redis 密码只进 .env。
- **事务**：answers+link_cards 多表写入使用事务。
- **超时**：所有外部调用（LLM/抓取/SMS/审核）独立超时；抓取并发 ≤5、单条 5s。
- **浏览器验收**：登录、提问、历史和详情四条核心路径使用 Playwright 做真实浏览器冒烟测试；交互后重新获取页面状态，保留失败截图与 trace。
- **依赖安全**：后端定期执行 `govulncheck ./...`，前端执行 `npm audit`；锁文件必须提交，依赖升级需单独审查。
- **内容渲染**：Markdown 禁止透传原始 HTML；链接仅允许 `http`/`https`，新标签页链接必须使用 `rel="noopener noreferrer"`。
- **Cookie 请求防护**：`/auth/refresh` 与 `/auth/logout` 依赖刷新 Cookie，除 `SameSite=Lax` 外还要校验 `Origin`/`Referer`，并要求自定义 CSRF 防护请求头。

---

## 8. 部署方案（2核2G）

```yaml
# docker-compose.yml 概要
services:
  nginx:     # 443/80 反代，限 body 2MB，读超时 180s（等 LLM）
  frontend:  # node:20-alpine, next start（standalone），内存限制 384M
  backend:   # golang 构建的 alpine 镜像单二进制，内存限制 128M
  mysql:     # mysql:8，innodb_buffer_pool≈300M，内存限制 640M
  redis:     # redis:7-alpine，maxmemory 64mb
```

- 主机配置 2G swap 兜底；MySQL 数据目录挂载卷并每日 mysqldump 备份（cron）。
- 七牛云仅存缩略图/上传图（量小，免费额度足够）。
- 上线前 checklist：域名备案、HTTPS 证书（certbot）、真实 SMS 密钥、天御密钥、LLM Key、CORS 白名单切换为正式域名。

**内存预算合计**：nginx 20M + 前端 250M + 后端 50M + MySQL 500M + Redis 50M ≈ 870M / 2048M。

---

## 9. 环境变量清单（.env.example，全部占位值）

```
APP_ENV=dev            # dev 时 SMS/审核走 Mock
HTTP_PORT=8080
MYSQL_DSN=user:pass@tcp(127.0.0.1:3306)/csqa?charset=utf8mb4&parseTime=true
REDIS_ADDR=127.0.0.1:6379
REDIS_PASSWORD=
JWT_SECRET=change-me
JWT_ACCESS_TTL=30m
JWT_REFRESH_TTL=720h
CORS_ORIGINS=http://localhost:3000
SMS_PROVIDER=mock      # mock | tencent
TENCENT_SMS_SECRET_ID= / SECRET_KEY= / SDKAPPID= / SIGN= / TEMPLATE_ID=
LLM_BASE_URL=https://api.deepseek.com/v1
LLM_API_KEY=sk-xxx
LLM_MODEL=deepseek-chat
LLM_TIMEOUT=120s
MODERATION_PROVIDER=mock   # mock | tencent
TENCENT_TMS_SECRET_ID= / TENCENT_TMS_SECRET_KEY=
QINIU_ACCESS_KEY= / SECRET_KEY= / BUCKET= / DOMAIN=
SENSITIVE_WORDS_FILE=./sensitive_words.txt
```

---

## 10. 里程碑

| 阶段 | 内容 | 产出 |
|---|---|---|
| M1 骨架 | 项目结构 + 配置校验 + 错误体系 + 日志 + 健康检查 + 迁移框架 | 可启动的空服务 |
| M2 认证 | SMS Mock → 登录/刷新/登出 全链路 | 可登录 |
| M3 问答 | LLM 客户端 + 提问/列表/详情 + 敏感词 | 核心闭环 |
| M4 媒体 | 链接抓取 + 卡片 + 七牛转存 | 富媒体展示 |
| M5 前后端联调与 UI | 四页面 + API 客户端 + 401 刷新 + UI 联调 | 完整 UI |
| M6 加固部署 | 天御接入 + docker-compose + nginx + 备份 | 可上线 |

## 11. 风险与对策

| 风险 | 对策 |
|---|---|
| LLM 附带的链接可能失效/幻觉 | 抓取失败降级为纯链接卡片；prompt 约束只引用知名站点；后续可加人工资源库 |
| 同步等待 LLM 时间长（10~30s） | 前端明确加载态；nginx 读超时 180s；二期可改异步轮询 |
| 2G 内存紧张 | 各容器内存限制 + swap + MySQL 调小 buffer pool |
| 公网合规（备案/审核） | 天御双层审核 + 上线前完成备案 |
| 单链接抓取拖慢响应 | 并发抓取、单条 5s 超时、失败不阻塞 |

---

以上为一期设计基线，按 M1 → M6 顺序实施。若调整 LLM 厂商默认值、表结构、接口契约或页面布局，应先修订对应文档并记录原因。
