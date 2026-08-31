# cs-qa-platform

计算机知识 AI 问答平台。用户通过手机号验证码登录后，可以提交计算机相关问题，获得结构化 Markdown 回答，并查看按需附带的视频、图片和普通链接资源卡片。

## 项目状态

- 一期状态：文档基线已完成，进入 M1 项目骨架开发
- 目标用户：计算机专业学生、在职开发者、计算机爱好者
- 目标规模：约 10 个注册用户，一期不要求并发和实时输出
- 部署方式：Docker Compose 单机双环境部署（test / production）
- 默认分支：`main`

## 文档入口

- [项目文档索引](欢迎.md)
- [需求分析文档](cs-qa-platform-需求分析文档.md)
- [设计方案](cs-qa-platform-设计方案.md)
- [技术栈文档](cs-qa-platform-技术栈文档.md)
- [UI 设计文档](cs-qa-platform-UI设计文档.md)
- [接口文档](接口文档.md)
- [Agent 协作指南](agent.md)
- [部署运维手册](部署运维手册.md)
- [测试计划](测试计划.md)
- [数据字典与迁移说明](数据字典与迁移说明.md)
- [安全威胁模型](安全威胁模型.md)

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.22+、Gin、sqlx |
| 前端 | Node.js 22 LTS、Next.js 14 App Router、TypeScript、Tailwind CSS |
| 数据 | MySQL 8.0、Redis 7 |
| 部署 | Nginx、Docker Compose、Ubuntu 24.04 LTS |
| 外部服务 | OpenAI 兼容协议 LLM、腾讯云 SMS、腾讯云天御、七牛云 Kodo |

## 本地环境

推荐安装：

- Git
- Docker Desktop（启用 WSL 2）
- Go 1.22 或更高版本
- Node.js 22 LTS 和 npm

本项目不要求本机直接安装 MySQL、Redis；开发依赖使用 Docker Compose 启动。

## 快速开始

以下命令以项目目录为当前目录。M1 骨架完成后，命令和服务名以实际 Compose 文件为准。

```bash
git clone https://github.com/JIUSANYI/-AI-.git
cd -AI-
cp backend/.env.example backend/.env
docker compose -f deploy/docker-compose.dev.yml up -d mysql redis
```

启动后端：

```bash
cd backend
go run .
```

启动前端：

```bash
cd frontend
npm ci
npm run dev
```

默认本地地址：

- 前端：`http://localhost:3000`
- 后端：`http://localhost:8080`
- 存活检查：`http://localhost:8080/health`
- 就绪检查：`http://localhost:8080/ready`

开发期建议使用 `SMS_PROVIDER=mock` 和 `MODERATION_PROVIDER=mock`。Mock 验证码只输出到后端日志，不通过接口返回。

## 常用质量检查

```bash
# backend
gofmt -w .
go test ./...
go vet ./...

# frontend
npm ci
npm run lint
npm run build
npm audit

# deploy
docker compose config
```

可用时追加 `govulncheck ./...` 和 Playwright CLI 浏览器冒烟测试。测试产物写入 `output/playwright/`，不要提交生成文件。

## GitHub Actions CI/CD

`.github/workflows/ci.yml` 会在 Pull Request 和推送到 `main` 时自动执行后端格式/测试/静态检查、前端 lint/build 以及 Docker Compose 配置校验。依赖漏洞扫描目前只做提示，不会因已有依赖风险阻断构建；升级依赖前需单独评估兼容性。

推送到 `develop` 会在 `test` Environment 中部署测试环境；推送到 `main` 会在 `production` Environment 中部署正式环境。两者都要求仓库变量 `DEPLOY_ENABLED=true`，正式环境还应配置必需的人工审批。部署前述 CI 检查必须全部通过，并需要在对应 GitHub Environment Secrets 中配置：

- `DEPLOY_HOST`：服务器地址
- `DEPLOY_USER`：SSH 用户名
- `DEPLOY_PORT`：可选，默认 `22`
- `DEPLOY_SSH_KEY`：对应服务器公钥的 SSH 私钥，多行原文保存

服务器目录分别是 `/home/ubuntu/cs-qa-platform-test` 和 `/home/ubuntu/cs-qa-platform-prod`，并且必须预先配置好 `backend/.env.test` 与 `backend/.env.prod`。工作流使用 `git pull --ff-only`，部署失败不会自动改写服务器上的本地提交或工作区。

## Git 约定

- `main` 保持可部署；功能使用短命 `feat/*` 或 `fix/*` 分支。
- 提交信息使用 Conventional Commits：`feat:`、`fix:`、`chore:`、`docs:`、`refactor:`。
- `.env`、访问令牌、验证码、API Key、数据库密码不得提交。
- 提交前检查 `git diff`，不要使用 `git push --force` 或 `git reset --hard`。

## 一期里程碑

`M1 骨架 → M2 认证 → M3 问答 → M4 媒体卡片 → M5 前端 → M6 部署加固`

## 许可

本项目使用 [MIT License](LICENSE)。
