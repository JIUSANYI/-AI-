# cs-qa-platform Agent 协作指南

## 1. 项目定位

`cs-qa-platform` 是面向计算机专业学生、开发者和计算机爱好者的计算机知识 AI 问答平台。

一期核心闭环：手机号验证码登录 → 提交计算机问题 → 双层内容审核 → 调用第三方 OpenAI 兼容协议大模型 → Markdown 结构化回答 → 按需抓取参考链接并展示媒体卡片 → 查看个人历史记录。

本文件是编码 Agent 的实施约定。需求、架构、技术栈和接口文档是项目上下文的权威来源：

- `cs-qa-platform-需求分析文档.md`：做什么、验收什么
- `cs-qa-platform-设计方案.md`：如何组织架构、数据和 API
- `cs-qa-platform-技术栈文档.md`：使用什么技术、如何管理依赖和 Git
- `接口文档.md`：前后端联调契约、字段与错误码

若实现需要偏离这些文档，先说明影响并同步修订相关文档；不要默默改变产品范围或核心架构。

## 1.1 当前执行范围：前后端实现，UI 由协作模型负责

当前阶段由本开发 Agent 负责 `backend/` 和 `frontend/` 的工程实现、联调、测试与部署配置维护。UI 视觉方向、页面布局、交互细节和视觉验收由另一个模型负责，本 Agent 不承担视觉设计。两方通过《接口文档》和本仓库提交协作，接口或页面契约变化必须同步文档。

## 2. 一期边界

必须完成：

- 手机验证码登录/自动注册、刷新令牌、登出
- AI 问答；问题和回答都要经过审核
- 回答按需附带 0～4 个真实参考链接
- URL 元数据抓取、视频/图片/普通链接卡片展示
- 个人历史列表（每页 20 条）和详情页
- `/health`、`/ready`、结构化日志、请求 ID
- Docker Compose 单机部署，适配 2 核 2G + 2G swap

明确不做：多轮上下文、流式输出、管理员后台/角色权限、社区互动、历史搜索、通知、原生 App、小程序、暗色主题、WebSocket/SSE、消息队列、微服务、Kubernetes、GraphQL、BFF 和前端状态管理库。

## 3. 技术基线

- 后端：Go 1.22+、Gin 1.10、MySQL 8.0、Redis 7
- 前端：Node.js 22 LTS、Next.js 14 LTS App Router、TypeScript 5 strict
- 前端依赖：Tailwind CSS、SWR、`react-markdown`、`remark-gfm`
- 后端依赖：`sqlx`、MySQL driver、`go-redis/v9`、`golang-jwt/v5`、腾讯云 SDK、七牛云 SDK；优先使用标准库
- 部署：Nginx 1.24 + Docker Compose，服务器隔离测试（8081）与正式（80/443）环境，服务为 nginx/frontend/backend/mysql/redis
- LLM：自写 OpenAI 兼容 HTTP 客户端，不引入 OpenAI SDK；Base URL、Key、Model 均配置化
- 外部服务开发期默认 Mock：SMS 和内容审核不应阻塞本地开发

新增依赖前必须回答：标准库能否实现？现有依赖能否实现？单人是否能维护？只有确有必要才添加，并提交锁文件变更。

## 4. 代码组织

仓库采用 Monorepo，推荐结构：

```text
docs/
backend/
  main.go
  .env.example
  migrations/
  internal/
    config/ errs/ middleware/ platform/
    modules/
      auth/ questions/ moderation/ llm/ media/
frontend/
  src/app/               # layout、提问、login、history、questions/[id]
  src/components/        # REPL、LinkCard、MarkdownView 等
  src/lib/api.ts
  src/lib/types.ts
  src/context/AuthContext.tsx
deploy/
  docker-compose*.yml
  nginx/
```

后端严格遵循单向分层：`handler（绑定/校验） → service（业务编排） → repository（SQL）`。控制器不放业务逻辑；外部服务通过薄接口封装，便于 Mock 和切换厂商；`main.go` 手工组装依赖，不引入 DI 框架。

数据库迁移使用版本化 SQL，并通过 `go:embed` 打进二进制，启动时执行并写入 `schema_migrations`。数据库使用 InnoDB/utf8mb4。当前核心表为 `users`、`refresh_tokens`、`questions`、`answers`、`link_cards`；验证码仅存 Redis，不建立运行时验证码表。历史迁移 `0002_auth.sql` 曾创建 `sms_codes`，`0005_drop_sms_codes.sql` 已清理该遗留表。

## 5. API 与业务规则

所有 API 使用 `/api/v1` 前缀、REST + JSON。核心路由：

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/auth/sms-code` | 发送验证码 |
| POST | `/auth/login` | 验证码登录/自动注册 |
| POST | `/auth/refresh` | Cookie 刷新访问令牌 |
| POST | `/auth/logout` | 吊销刷新令牌并清 Cookie |
| POST | `/questions` | 同步提问并返回回答及卡片 |
| GET | `/questions?page&size` | 当前用户历史，默认每页 20 条 |
| GET | `/questions/:id` | 当前用户问答详情 |
| GET | `/health`、`/ready` | 健康/就绪检查 |

统一响应使用：

```json
// success
{"data": {}, "request_id": "..."}
// error
{"error": {"code": "INVALID_SMS_CODE", "message": "验证码错误或已过期"}, "request_id": "..."}
```

实现时必须：

- 手机号、验证码、问题长度在服务端校验；问题为 1～2000 字符
- 验证码 60 秒限发、有效期不超过 10 分钟、一次性使用、每日每号最多 5 条；必要时叠加 IP 限流
- 访问令牌有效期 30 分钟，仅存前端内存；刷新令牌有效期 30 天，httpOnly Cookie 下发，服务端只存 SHA-256 哈希且支持轮换/吊销
- 未登录返回 401；越权数据返回 403 或 404，不泄露他人数据
- 提问先本地敏感词过滤，再云端天御审核；拒绝时不调用 LLM，记录 `rejected` 和拒绝原因类别
- LLM 超时 120 秒；失败记录 `failed`，给出可重试错误，不返回空白回答
- 回答审核通过后才可向用户展示和落库为可见回答
- LLM 仅在确有帮助时输出参考资源，最多 4 个；简单问题允许 0 个
- 链接最多处理 4 个；仅允许 http/https 公网地址，阻断内网、环回、云元数据等 SSRF 目标；单链接 5 秒超时、限制响应体和重定向次数
- 链接抓取失败不阻塞回答，降级为纯链接；缩略图转存七牛异步执行，未配置或失败时保留原图 URL
- `answers + link_cards + question.status` 使用事务写入

## 6. 认证与请求层

前端访问令牌放在 `AuthContext`/模块级内存变量中，严禁写入 `localStorage`。刷新页面时通过 httpOnly Cookie 静默调用 refresh 恢复会话。API 封装集中在 `src/lib/api.ts`：自动附加令牌，401 只刷新并重放一次；4xx 不自动重试，5xx 最多重试 3 次，网络失败显示离线提示。

后端错误使用类型化错误（`Code`、HTTP 状态、用户可见 `Message`），统一中间件渲染；客户端绝不收到堆栈。日志使用 JSON 结构化格式并贯穿 `request_id`，手机号脱敏，禁止记录令牌、验证码和密钥。

## 7. UI 实施规范

视觉方向是“讲义纸 × 终端（Lab Notebook × Terminal）”：浅色工程方格纸背景、REPL 提问窗作为唯一视觉爆点、回答以讲义卡呈现。不要使用紫色渐变、通用聊天气泡、深色霓虹主题、阴影堆叠或无关装饰。

- 颜色令牌通过 CSS 变量定义，业务类不得写裸 hex：`paper #F7F9FA`、`ink #1C2733`、`circuit #2A5CAA`、`pass #1F7A4D`、`signal #D9730D`、`comment #8593A8`
- 顶栏 56px，内容单列最大宽度 720px；卡片靠边框、底色和间距分层，不使用阴影
- 自托管 Noto Sans SC、LXGW WenKai Screen（子集化）、JetBrains Mono；禁止外部字体 CDN
- 回答正文使用文楷 15px、行高 1.85；代码块等宽字体并支持横向滚动；Markdown 支持表格、代码和引用
- REPL 输入：固定 `❯` 提示符；Enter 发送，Shift+Enter 换行；移动端显示“发送 ⏎”；上限 2000 字符
- 媒体卡片类型：可嵌入站点为 `video`，图片直链/图片元数据为 `image`，其余为 `link`；桌面最多两列，移动端一列；视频点击后再展开 iframe，不自动加载播放
- 页面：`/login`、`/`、`/history`、`/questions/[id]`
- 所有交互元素键盘可达，保留清晰 focus-visible；表单错误使用文字 + `aria-invalid`；iframe 必须有 `title`，图片必须有 `alt`
- 动效只在 `prefers-reduced-motion: no-preference` 下启用，总时长不超过 300ms；不做滚动视差和装饰性循环动画
- UI 文案使用“发送”“正在编译你的问题…”“我的提问”等约定，不使用“AI 思考中”“提交”“生成”、表情符号或感叹号

## 8. 配置与安全红线

所有密钥必须来自环境变量，`.env` 不入库，`.env.example` 只放占位值。至少支持：`APP_ENV`、`HTTP_PORT`、`MYSQL_DSN`、`REDIS_*`、`JWT_*`、`CORS_ORIGINS`、`SMS_PROVIDER`、`LLM_*`、`MODERATION_PROVIDER`、腾讯云 SMS/天御凭证、七牛云凭证、`SENSITIVE_WORDS_FILE`。

生产环境必须显式配置 CORS 白名单、HTTPS、安全响应头、真实 SMS/天御/LLM 凭证和域名；Nginx 请求体限制 2MB、读超时约 180 秒。不得把密钥、验证码、令牌、手机号明文写入代码、测试输出或日志。

## 9. 开发顺序与质量门禁

按 M1 → M6 推进：

1. M1：项目骨架、配置校验、错误体系、日志、健康检查、迁移框架
2. M2：Mock 短信、登录/刷新/登出全链路
3. M3：LLM 客户端、提问、审核、历史列表/详情
4. M4：URL 抓取、媒体卡片、七牛转存
5. M5：四页面、Markdown、响应式 UI、401 刷新
6. M6：天御、Docker Compose、Nginx、备份和上线加固

每完成一个可运行的小步就提交一次 Git。提交信息遵循 Conventional Commits（`feat:`、`fix:`、`chore:`、`docs:`、`refactor:`），先检查 `git diff` 再提交；不要用 `reset --hard` 或强推破坏历史。里程碑完成后打语义化 tag。

每次完成代码修改后，都必须先完成一次自审，再决定是否提交。自审不是只看新增行，必须检查完整 `git diff`、被修改模块的相邻调用链、接口契约和相关文档。审查顺序固定为：

1. 变更范围：确认没有误改、调试代码、临时文件、密钥或无关格式化；执行 `git diff --check`。
2. 需求正确性：逐条对照需求、设计和接口文档，确认成功路径、返回值、状态变化和兼容性。
3. 失败与边界：检查空值、非法输入、重复请求、超时、依赖不可用、部分失败、重试和资源释放。
4. 安全：检查认证、授权/越权、注入、XSS、CSRF、SSRF、敏感信息、日志脱敏、限流和错误信息泄露。
5. 并发与运行时：检查竞态、事务边界、锁、超时、连接/响应体限制、goroutine/定时器泄漏和重试风暴。
6. 测试与可维护性：确认已有测试仍覆盖，新增分支有对应测试，命名/分层/依赖符合本指南。
7. 联动更新：确认 API、数据字典、部署配置、README 和测试计划是否需要同步。

代码审查不通过时，必须先修复再重新审查；不得以“后续再改”为理由提交或推送。无法执行某项检查时，要在交付报告中明确原因和风险，不得声称检查已通过。

提交前至少执行并修复结果：

- 后端：`gofmt`、`go test ./...`、`go vet ./...`
- 后端安全：`govulncheck ./...`（工具可用时）
- 前端：执行 lint、build、类型检查和 Playwright 冒烟；视觉验收由 UI 协作模型完成
- 浏览器：使用 Playwright CLI 冒烟验证登录、提问、历史和详情；失败产物写入 `output/playwright/`
- 部署：`docker compose config`；可用时执行健康检查和关键链路冒烟测试

自动检查由 `.github/workflows/ci.yml` 执行。工作流检查后端与前端；Pull Request 必须通过后才能合并；`develop` 部署测试环境，`main` 部署正式环境，正式部署必须经过 GitHub `production` Environment 审批。工作流不得保存或回退服务器密码，服务器登录统一使用 GitHub Secret 中的 SSH 私钥。

重点测试纯逻辑：JWT 签发/校验、审核异常时的默认拒绝降级、URL/media_type 判定、SSRF 拦截、验证码过期/复用/限频、用户越权访问。

## 10. Agent 工作方式

开始编码前先阅读本指南及四份项目文档，确认当前里程碑和已有改动；先检查工作树，保留用户现有修改，不覆盖无关文件。

实现优先选择最小、可维护的改动：不要为了未来需求预建抽象，不要擅自扩展一期范围，不要引入未批准的库。对外部服务提供接口和 Mock；对失败、超时、空结果、降级路径明确处理。

每次完成后报告：改动文件、实现的业务规则、自审结论、验证命令及结果、已知风险或未完成项。自审至少回答“是否检查完整 diff”“是否检查安全和失败路径”“是否同步文档”“哪些检查未执行”。若文档存在冲突、外部凭证缺失或操作会扩大范围，暂停相关实现并说明原因；可用 Mock/降级继续推进的，不应被凭证阻塞。

### 可用技能

- `playwright`：真实浏览器联调、页面状态检查、截图和 trace。
- `security-best-practices`：Go、Next.js 和 React 的安全基线检查。
- `security-threat-model`：在明确要求威胁建模时，梳理资产、边界、攻击路径和缓解措施。
- `screenshot`：浏览器工具无法覆盖时进行系统级截图；优先使用 Playwright 自带截图。

技能用于加强验证，不能替代项目文档。技能建议与已确认架构冲突时，应说明差异并先更新文档。
