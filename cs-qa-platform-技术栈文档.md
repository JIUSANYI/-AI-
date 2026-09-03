# 计算机知识 AI 问答平台 · 技术栈文档

| 文档属性 | 内容 |
|---|---|
| 项目名称 | cs-qa-platform |
| 文档版本 | v1.1 |
| 编写日期 | 2026-08-30（2026-08-31 修订） |
| 文档状态 | 已确认，实施中 |
| 关联文档 | 《需求分析文档》《设计方案》《UI 设计文档》《接口文档》 |
| 开发模式 | **前后端协同开发**（本 Agent 负责工程实现，UI 协作模型负责视觉设计与验收） |

---

## 1. 选型总原则（小团队与 AI 协作视角）

所有技术决策服从以下优先级，遇到冲突时序号小者胜：

| # | 原则 | 含义 |
|---|---|---|
| 1 | **单人可维护** | 宁选"社区大、文档全、出问题搜得到"的成熟技术，不追新 |
| 2 | **依赖最少化** | 每引入一个第三方库都要写清理由；能用标准库/平台能力解决的不引库 |
| 3 | **一套心智模型** | 前后端各一个框架走到底，不混搭状态管理/ORM 等流派对 |
| 4 | **2C2G 现实约束** | 一切以单机 2核2G 内存预算（约 900MB 服务内存）为硬边界 |
| 5 | **可被 AI 辅助** | 主流技术栈 AI 生成代码质量高，冷门技术得不偿失 |

**明确不引入的技术**（一期约 10 用户规模下属于负资产）：

- 消息队列（Kafka/RabbitMQ）、微服务/服务网格、Kubernetes
- GraphQL、tRPC、BFF 层
- 前端状态管理库（Redux/Zustand/Jotai）——React Context + SWR 级别足够
- 灰度发布、多环境流水线、独立 CI 集群

---

## 2. 后端技术栈（Go）

### 2.1 语言与运行时

| 项 | 选型 | 版本 | 理由 |
|---|---|---|---|
| 语言 | Go | 1.22+ | 编译单二进制、内存占用 ~50MB、部署即拷贝；标准库覆盖 HTTP/JSON/加密，天然符合原则 1/2/4 |
| 包管理 | Go Modules（内置） | — | 无额外工具链 |

### 2.2 依赖清单（全部理由化）

| 依赖 | 版本 | 用途 | 替换成本 |
|---|---|---|---|
| `github.com/gin-gonic/gin` | v1.10 | HTTP 路由/中间件/参数绑定 | 低（net/http 可平替） |
| `github.com/jmoiron/sqlx` | v1.4 | 在 `database/sql` 上加结构体扫描 | 极低（薄封装，随时退回标准库） |
| `go-sql-driver/mysql` | v1.8 | MySQL 驱动 | 必需 |
| `github.com/redis/go-redis/v9` | v9.x | Redis 客户端 | 必需 |
| `github.com/golang-jwt/jwt/v5` | v5.x | JWT 签发/校验 | 低 |
| `github.com/tencentcloud/tencentcloud-sdk-go` | 最新稳定 | SMS + 天御审核（按需安装子包） | 中（SDK 系厂商绑定） |
| `github.com/qiniu/go-sdk/v7` | v7.x | 七牛云存储（仅缩略图转存） | 低 |
| `golang.org/x/time/rate` | x 包 | 验证码/接口限流 | 极低 |

**刻意不用**：

- **GORM/ent 等 ORM**：SQL 直写 + sqlx 扫描，迁移文件即文档；6 张表规模下 ORM 的抽象成本大于收益（原则 3）
- **wire/dig 等依赖注入框架**：`main.go` 手工组装依赖，30 行以内，可视化且零魔法
- **viper**：环境变量用标准库 `os.Getenv` + 自写 120 行集中校验，足够
- **zap/zerolog**：标准库 `log/slog`（结构化日志，Go 1.21+ 内置）完全够用

### 2.3 项目内技术约定

| 项       | 约定                                                                                               |
| ------- | ------------------------------------------------------------------------------------------------ |
| API 风格  | REST + JSON，统一前缀 `/api/v1`；成功返回 `{data, request_id}`，失败返回 `{error: {code, message}, request_id}` |
| 分层      | handler（绑定/校验）→ service（业务）→ repo（SQL），单向依赖                                                      |
| 错误处理    | 自建 `errs` 类型化错误（Code + HTTPStatus + Message），handler 统一渲染                                        |
| 迁移      | 版本化 SQL 文件 `go:embed` 进二进制，启动时自动执行 + `schema_migrations` 表记录                                     |
| LLM 调用  | 不用任何 OpenAI SDK，自写 ~150 行 OpenAI 兼容协议 HTTP 客户端（标准库即可完成），换厂商零成本                                   |
| 链接抓取    | 标准库 `net/http` + `golang.org/x/net/html` 解析 og/meta；自实现 SSRF 内网地址拦截                              |
| 测试      | 优先覆盖 token、验证码、审核降级、SSRF、越权和 media_type 判定；HTTP 关键路径使用 `httptest`                                |
| 静态与安全检查 | `go vet` + `golangci-lint`（errcheck/staticcheck/govet）+ `govulncheck ./...`                      |

---

## 3. 前端技术栈（Next.js）

### 3.1 框架与语言

| 项 | 选型 | 版本 | 理由 |
|---|---|---|---|
| 框架 | Next.js（App Router） | 14 LTS | 路由/布局/构建一体；`output: standalone` 后 Node 进程 ~250MB，2C2G 可承受 |
| 语言 | TypeScript | 5.x（strict） | 类型即文档，减少多模型协作中的接口和实现偏差 |
| 运行时 | Node.js | 22 LTS | 与 Next.js 14 兼容的长期支持版 |

### 3.2 依赖清单

| 依赖 | 用途 | 说明 |
|---|---|---|
| `tailwindcss` | 样式（配合 CSS 变量令牌，见 UI 设计文档 §11） | 无运行时成本 |
| `react-markdown` + `remark-gfm` | AI 回答 Markdown 渲染（服务端组件内渲染） | GFM 支持表格/删除线 |
| ` SWR`（`swr`） | 列表/详情数据请求（缓存 + 重新验证） | ~4KB，替代整个状态管理层 |
| — | 其余一律不引 | 无 UI 组件库（自绘组件才符合 UI 设计文档）、无表单库（原生受控组件足够）、无动画库（CSS 原生过渡） |

### 3.3 项目内技术约定

| 项 | 约定 |
|---|---|
| 组件策略 | 能服务端渲染的全服务端（Markdown、列表首屏）；仅 REPL 提问窗、登录表单、媒体卡片交互为客户端组件 |
| 请求层 | 自写 `lib/api.ts`（~100 行）：fetch 封装 + 自动附带访问令牌 + 401 时单次刷新重试 |
| 认证态 | 访问令牌存内存模块级变量 + `AuthContext`；刷新令牌 httpOnly Cookie 由后端管理 |
| 字体 | `next/font/local` 自托管三个字体（Noto Sans SC / 霞鹜文楷子集化 / JetBrains Mono），禁外部 CDN |
| Lint 与安全 | ESLint（`next/core-web-vitals`）+ Prettier + `npm audit`；CI 使用 `npm ci` 保证可复现安装 |
| 浏览器验收 | 使用 Playwright CLI 验证登录、提问、历史、详情和响应式布局；失败产物写入 `output/playwright/` |

---

## 4. 数据与缓存

| 项 | 选型 | 版本 | 用途 | 备注 |
|---|---|---|---|---|
| 数据库 | MySQL | 8.0（InnoDB / utf8mb4） | 全部业务数据（6 张表） | 容器 `--memory 600M`，连接池上限 10 |
| 缓存 | Redis | 7.x | 验证码计数、限流计数、刷新令牌吊销名单 | `maxmemory 128M` + `allkeys-lru`，容器 ~50M |

> Redis 不做缓存层、不做队列——它在这套系统里只有三个明确用途，不给自己制造"数据一致性"问题。

---

## 5. 基础设施与外部服务

| 层 | 选型 | 说明 |
|---|---|---|
| 反向代理 | Nginx 1.24（容器） | TLS 终结、静态资源、Next/Go 反代；~20MB |
| 部署 | Docker Compose（单机） | 5 个容器：nginx / frontend / backend / mysql / redis；每服务 `mem_limit` + 2G swap 兜底 |
| 镜像仓库 | 阿里云容器镜像服务（免费个人版）或腾讯云 TCR 个人版 | 服务器在境内拉取快 |
| 大模型 | 第三方 API（OpenAI 兼容协议） | Base URL / Key / Model 全环境变量化，DeepSeek / 混元 / 通义可随时切换 |
| 短信 | 腾讯云 SMS | 开发期 Mock 模式（验证码打日志） |
| 内容审核 | 腾讯云天御（文本） | 开发期 Mock 模式（直接放行 + 日志） |
| 对象存储 | 七牛云 Kodo | 仅缩略图转存；未配置时降级用原图 URL |
| DNS/CDN | 一期不用 CDN | 10 用户流量走源站足够 |

---

## 6. 开发工具链（前后端协同工作流）

| 环节 | 工具 | 说明 |
|---|---|---|
| 密钥管理 | `.env`（gitignore）+ `.env.example`（提交占位） | 所有密钥只进环境变量，永不进库 |
| 本地依赖 | MySQL/Redis 一律 `docker compose -f docker-compose.dev.yml up` | 本机不装数据库，环境可随时销毁重建 |
| AI 辅助约定 | 需求/设计/UI/技术栈/接口/交接文档是模型的上下文基线，改代码先改文档 | 多模型协作的最大风险是文档漂移，此条为硬纪律 |
| 备份 | `mysqldump` 每日凌晨 cron → 服务器本地保留 7 份 | 数据量极小（<10MB/天），暂不上云 |

### 6.1 Git 版本管理（强制，不可选）

**仓库策略**：项目根目录 `cs-qa-platform/` 单一 Monorepo（文档 + backend + frontend 同库），初始化即 `git init`，第一天起所有变更走 Git。

| 项 | 约定 |
|---|---|
| 仓库结构 | `docs/`（四份设计文档）、`backend/`、`frontend/`、`deploy/`（compose/Dockerfile/nginx 配置） |
| 远程仓库 | **必配**：GitHub 私有仓库为主选（异地备份 + 断点续传的唯一真身）；网络不稳时备选 Gitee 私有仓库，二者可同时配置双远程 |
| 分支模型 | `main`（随时可部署的稳定版）+ 短命 `feat/*`、`fix/*` 分支；每个模型完成后自审 `git diff`，再由主负责人合并 |
| 提交规范 | Conventional Commits：`feat:` `fix:` `chore:` `docs:` `refactor:`；一次提交只做一件事；提交信息由 AI 辅助生成后人工过目 |
| 提交粒度 | 每完成一个可运行的小步（一个端点、一个组件、一处文档修订）即提交；禁止"攒一周提交一次" |
| 标签 | 每个里程碑完成打 tag：`v0.1.0-m1`（骨架）→ `v0.2.0-m2`（认证）→ … → `v1.0.0`（上线），语义化版本 |
| `.gitignore` | 必须包含：`.env`、`node_modules/`、`.next/`、`backend`/`bin/`、`*.log`、`dist/`、`uploads/`、`.DS_Store`；Obsidian 等编辑器目录不入库 |
| 密钥红线 | 提交前自查 `git diff` 中无密钥；一旦误提交密钥，立即吊销该密钥（不是删除提交就完事） |
| 提交流程 | `main` 分支禁止直接 `git push --force`；回退一律用 `git revert`，不用 `reset --hard` 污染历史 |
| 里程碑归档 | 每个里程碑合并后，将当时的文档目录打快照 tag（`docs-v1.0`），文档演进有据可查 |

**最小 .gitignore 首版**（实施 M1 时随骨架一起提交）：

```gitignore
.env
.env.local
node_modules/
.next/
out/
bin/
dist/
*.log
uploads/
.DS_Store
Thumbs.db
```

---

## 7. 版本总览（一张表）

```
语言运行时    Go 1.22+          Node.js 22 LTS
框架         Gin v1.10          Next.js 14 LTS (App Router)
语言扩展     —                 TypeScript 5 (strict)
数据         MySQL 8.0          Redis 7
核心库       sqlx / go-redis9   tailwindcss / swr / react-markdown
             golang-jwt/v5      remark-gfm
             tencentcloud-sdk   next/font (local)
             qiniu go-sdk/v7
基础设施     Nginx 1.24 + Docker Compose（单机 2C2G）
版本控制     Git（Monorepo 单仓，GitHub 私有仓库远程备份，Conventional Commits）
外部服务     LLM API(OpenAI兼容) / 腾讯云SMS / 腾讯云天御 / 七牛云Kodo
```

---

## 8. 依赖治理规则（长期纪律）

1. **新增依赖需过三问**：标准库能不能做？现有依赖能不能做？引入后我一个人看得懂它的源码吗？
2. **版本升级纪律**：只追 LTS/稳定大版本；每月固定一天统一 `go get -u` / `npm outdated` 检查，不零散升级。
3. **锁文件必提交**：`go.sum` 与 `package-lock.json` 进版本库，保证任何时间点可复现构建。
4. **弃用即删除**：不再使用的依赖当周移除，不留"以后可能用得上"。

---

## 9. 技术风险与对策

| 风险 | 对策 |
|---|---|
| Next.js 大版本升级破坏 App Router 用法 | 锁 14 LTS 不动；项目页面少（4 个），真要升级迁移面小 |
| 霞鹜文楷子集化流程踩坑 | 备选方案：回退 Noto Sans SC（仅正文观感降级，无功能损失） |
| 腾讯云 SDK 体积大、API 变动 | SMS/天御调用收敛到独立包内薄封装，Mock 模式常开，SDK 升级只动一处 |
| 协作中断风险 | 核心文档 + 交接说明 + Conventional Commits + 简洁分层，任何时点可被其他模型接续 |
| 2C2G 内存溢出 | 每容器 mem_limit + swap；MySQL 连接池 10；Next standalone；Go 天然省内存 |

---

*本文档与《设计方案》配套：设计方案定"怎么架构"，本文档定"用什么造、依赖怎么管"。实施期间如需偏离，先修订本文档。*
