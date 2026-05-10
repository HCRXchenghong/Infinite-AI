# Infinite-AI

Infinite-AI 是一套全栈 AI 服务系统，包含类 ChatGPT 的网页端、后台管理端、兑换与会员体系、OpenAI/Anthropic 兼容 API、多模态聊天、图片生成/编辑、联网搜索，以及多供应商路由与自动降级能力。

项目采用前后端分离与多服务架构：前端基于 React/Vite，后端服务使用 Go，数据持久化使用 PostgreSQL，Redis 负责会话与限流状态，MinIO 存储附件，NATS 支撑后台任务，SearXNG 作为本地联网搜索兜底服务。

## 功能特性

- 聊天会话：支持流式运行事件、主动取消、历史消息、附件、图片视觉输入、Artifacts、会话分享与共享协作。
- 模型路由：支持 OpenAI 兼容协议与 Anthropic 兼容协议，可配置多端点顺序调用、自动探测 endpoint 形态，并将适配结果持久化，避免重启后重复慢探测。
- 联网搜索：优先调用 OpenAI Responses Web Search / Anthropic Web Search 等官方原生工具；不可用时自动降级到 SearXNG，并带搜索词规划、相关性评分、去重和二次抓取。
- 图片能力：支持图片生成、图片编辑、参考图生成、提示词规划，以及普通聊天中的图片附件分析。
- 开放 API：提供 `/v1/chat/completions`、`/v1/responses`、`/v1/messages`、`/v1/images/generations`、`/v1/images/edits` 等兼容接口。
- 后台管理：支持用户、模型路由、套餐限制、系统日志、服务告警、OAuth、邮件/短信网关、搜索服务、财务配置、兑换活动等管理能力。
- 用户体系：支持注册登录、验证码、人机校验、OAuth 登录、API Key、套餐订阅、用量查询、数据导出与账号注销。

## 目录结构

```text
apps/web                 React + TypeScript + Vite 网页端
db/migrations            PostgreSQL 数据库结构
deploy                   Docker Compose、Dockerfile、Nginx、SearXNG 配置
services/bff             对外 BFF，负责会话、CSRF、认证、OAuth、代理转发
services/core            核心业务 API，负责聊天、后台、账单、开放 API
services/shared          Go 共享包，包括配置、认证、数据库、Store、HTTP 工具
services/worker          后台维护任务服务
```

## 快速启动

环境要求：

- Docker 与 Docker Compose，用于启动完整本地服务栈
- Go 1.25+，用于本地后端构建与测试
- Node.js 25+ 与 npm，用于本地前端开发

启动完整本地环境：

```bash
docker compose -f deploy/docker-compose.yml up --build
```

本地访问地址：

```text
用户端：       http://127.0.0.1:1001
API/BFF：      http://127.0.0.1:1002
后台管理端：   http://127.0.0.1:1003
兑换页：       http://127.0.0.1:1004
MinIO 控制台： http://127.0.0.1:1007
SearXNG：      http://127.0.0.1:1011
```

Docker Compose 默认使用本地开发凭据。若要部署到公网或共享环境，请务必替换所有密钥和默认账号。

## 本地开发

后端测试与构建：

```bash
go test ./services/...
go build ./services/...
```

前端开发与构建：

```bash
cd apps/web
npm install
npm run dev
npm run build
```

如需覆盖默认环境变量，可以复制 `.env.example`：

```bash
cp .env.example .env
```

## 默认本地管理员

```text
邮箱：      admin@infinite.local
密码：      ChangeThisAdminPassword123!
TOTP 密钥： JBSWY3DPEHPK3PXP
```

请将 TOTP 密钥导入任意身份验证器应用，用于生成后台登录的 MFA 验证码。以上账号仅用于本地开发，生产环境必须替换。

## 配置说明

- 模型供应商在后台管理端的模型配置中维护，endpoint 与 key 会加密存入数据库。
- OpenAI 兼容供应商会自动探测最合适的 endpoint 形式，成功结果会写入数据库，服务重启后不会重新慢探测。
- 联网搜索优先走供应商原生工具；官方能力不可用时，才会降级到本地 SearXNG。
- IF-Pay 支付订单会真实入库；未配置真实商户凭据时，支付流程会返回待配置状态，不会伪造支付成功。

## 上线前检查

- 替换 `INTERNAL_JWT_SECRET`、`COOKIE_HASH_SECRET`、`MASTER_KEY`、数据库密码、MinIO 密钥和默认管理员密码。
- 使用 HTTPS 暴露服务，并正确配置 `PUBLIC_BASE_URL`、`API_BASE_URL`、`ADMIN_BASE_URL`、`REDEEM_BASE_URL`。
- 为 PostgreSQL、Redis、MinIO 和日志配置持久化存储与备份策略。
- 在后台管理端配置真实 OAuth、邮件/短信网关、支付凭据和模型供应商 endpoint。
- 不要提交 `.env`、私钥、供应商 key、数据库 dump 或备份文件。项目根目录已配置 `.gitignore` 与 `.dockerignore` 来降低误提交风险。
