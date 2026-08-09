# AGENTS.md

## 项目概览

这是一个中文个人技术博客 MVP，使用 Go 单体应用和服务端渲染。项目目标是保持部署简单：运行时只需要一个 Go 二进制、SQLite 数据库文件和上传目录，不依赖 Node.js、外部数据库或独立前端服务。

主要功能：

- 前台：首页、文章列表与分页、文章详情、分类/标签筛选、Markdown、代码高亮、响应式布局和基础 SEO。
- 后台：单管理员登录、文章 CRUD、草稿/发布状态、文章内选择或新建分类标签、图片上传和网站设置。
- 后台布局：桌面端固定左侧栏，可折叠为图标栏；移动端使用抽屉侧栏；折叠状态保存在浏览器 `localStorage`。
- 安全：bcrypt 密码哈希、数据库会话、HttpOnly Cookie、CSRF、登录限流、安全响应头和上传 MIME 校验。

## 技术栈与结构

- Go 1.26，模块名 `ffzd.site/blog`。
- HTTP：`net/http` + `github.com/go-chi/chi/v5`。
- 数据库：SQLite + `modernc.org/sqlite`，纯 Go 驱动，不依赖系统 `sqlite3`。
- Markdown：Goldmark；代码高亮：Goldmark Highlighting + Chroma。
- 页面：Go `html/template`、原生 CSS 和原生 JavaScript。
- 模板、静态资源和 SQL 迁移通过 `embed` 编译进二进制。

关键位置：

- `cmd/blog/main.go`：应用入口、信号处理和优雅关闭。
- `internal/app/config.go`：环境变量和持久目录初始化。
- `internal/app/http.go`：应用组装、路由、中间件及 HTTP handlers。
- `internal/app/store.go`：SQLite 迁移、查询和数据写入。
- `internal/app/markdown.go`：Markdown 渲染和 slug 生成。
- `internal/app/templates/`：前后台页面模板；`partials.html` 包含共享导航和后台壳层。
- `internal/app/static/`：全站 CSS 和 JavaScript。
- `internal/app/migrations/`：按数字前缀排序的版本化 SQL 迁移。

## 数据与路由约定

数据库包含 `admins`、`sessions`、`posts`、`categories`、`tags`、`post_tags` 和 `site_settings`。文章 Markdown 是内容源，保存时同步生成 HTML。已发布文章的 slug 在首次发布后锁定；草稿不得出现在公开查询、详情页或 sitemap 中。

公开路由：

- `/`
- `/posts`
- `/posts/{slug}`
- `/categories/{slug}`
- `/tags/{slug}`
- `/sitemap.xml`
- `/robots.txt`
- `/uploads/*`

后台路由统一位于 `/admin`。除 `/admin/login` 外均需有效会话；所有非 GET 后台请求必须通过 CSRF 校验。

分类和标签没有独立管理页面。已有项在文章编辑器中选择，新项通过同一表单创建，并与文章保存放在同一个数据库事务中。

新增数据结构时使用新的迁移文件，例如 `002_feature_name.sql`，不要修改已发布迁移。数据库、上传文件和运行日志属于本地状态，不应提交到版本控制。

## 开发约定

- 保持服务端渲染和单体架构；没有明确需求时不要引入 SPA 或 Node.js 构建链。
- 前台和后台共享设计变量，但后台样式应放在 `.admin-layout` 等后台作用域内，避免影响前台。
- 修改后台页面时保留侧栏、顶部栏、活动菜单状态、桌面折叠和移动抽屉行为。
- 分类和标签的创建入口保持在文章编辑器中，不要重新增加独立管理菜单；新分类、标签与文章关联必须原子保存。
- 修改 Markdown 时必须继续禁用原始 HTML，并保证后台预览与前台使用同一个渲染器。
- 上传只允许 PNG、JPEG、GIF 和 WebP，默认上限 10 MB；不要根据用户原始文件名生成保存路径。
- SQL 使用参数绑定；跨多表写操作使用事务；保持 SQLite 外键开启。
- 不要把管理员密码、会话密钥或其他凭据写进代码、文档或仓库。
- 使用 `gofmt` 格式化 Go 文件。前端保持原生 CSS/JS，不引入依赖前先说明必要性。

## 配置与运行

应用直接读取环境变量，不自动加载 `.env`：

- `APP_ADDR`：默认 `127.0.0.1:8080`。
- `DATABASE_PATH`：默认 `data/blog.db`。
- `UPLOAD_DIR`：默认 `uploads`。
- `ADMIN_USERNAME`、`ADMIN_PASSWORD`：仅数据库中没有管理员时必填；密码至少 12 位。
- `SESSION_SECRET`：部署时必须固定设置；缺省会临时随机生成，重启后会使现有会话失效。

首次本地运行示例：

```powershell
$env:ADMIN_USERNAME='admin'
$env:ADMIN_PASSWORD='替换为至少12位的强密码'
$env:SESSION_SECRET='替换为随机长字符串'
go run ./cmd/blog
```

常用检查：

```powershell
gofmt -w cmd internal
go test ./...
go vet ./...
go build -o blog.exe ./cmd/blog
```

## 测试与交付要求

- 所有 Git 提交信息必须使用中文；提交前检查暂存内容，禁止提交数据库、上传文件、日志、构建产物或凭据。
- Markdown 或 slug 变更：更新 `markdown_test.go`。
- 数据模型、查询或迁移变更：使用临时 SQLite 数据库更新 `store_test.go`。
- 路由、认证、模板或安全行为变更：更新 `http_test.go`。
- 每次交付至少运行 `go test ./...`、`go vet ./...` 和一次完整构建。
- 页面布局变更需浏览器检查桌面端和约 390px 移动端，确认无横向溢出、活动菜单正确、抽屉可关闭且控制台无错误。
- 禁止使用 Codex 应用内浏览器工具（`browser:control-in-app-browser`）进行页面检查或自动化，避免再次引发 Codex 客户端闪退；改用独立浏览器自动化、HTTP 检查或人工浏览器验证。
- 当前环境未配置 Midscene 所需的视觉模型环境变量时，禁止尝试使用 `browser-automation`（Midscene）技能进行验收，也不要在后续任务中反复调用；应改用 HTTP 检查、其他无需该视觉模型的独立浏览器方案，或明确说明需要人工浏览器验证。
- 完成修改和检查后必须重启本地服务，确保运行的是最新代码或构建产物。替换正在运行的本地二进制前，先用候选端口验证；重启和部署过程中保留 `data/blog.db` 和 `uploads/`，不得清空用户数据。
