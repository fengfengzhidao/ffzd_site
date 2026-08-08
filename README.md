# Go 个人技术博客

一个使用 Go、SQLite 和服务端模板构建的单管理员技术博客 MVP。支持 Markdown、代码高亮、文章发布、分类标签、图片上传和基础 SEO。

## 本地运行

需要 Go 1.26 或兼容版本。首次启动必须提供管理员账号和至少 12 位密码：

```powershell
$env:ADMIN_USERNAME='admin'
$env:ADMIN_PASSWORD='替换为你的强密码'
$env:SESSION_SECRET='替换为随机长字符串'
go run ./cmd/blog
```

打开：

- 前台：<http://127.0.0.1:8080/>
- 后台：<http://127.0.0.1:8080/admin/>

首次启动后，管理员密码会以 bcrypt 哈希写入 `data/blog.db`，后续启动无需再次设置账号变量。删除数据库会触发重新初始化。

## 配置

应用直接读取系统环境变量，不会自动加载 `.env`。完整示例见 `.env.example`。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `APP_ADDR` | `127.0.0.1:8080` | HTTP 监听地址 |
| `DATABASE_PATH` | `data/blog.db` | SQLite 文件路径 |
| `UPLOAD_DIR` | `uploads` | 上传图片目录 |
| `ADMIN_USERNAME` | 无 | 首次初始化必填 |
| `ADMIN_PASSWORD` | 无 | 首次初始化必填，至少 12 位 |
| `SESSION_SECRET` | 临时随机值 | 建议固定设置，重启后保持一致 |

## 验证与构建

```powershell
go test ./...
go vet ./...
go build -o blog.exe ./cmd/blog
```

数据库迁移、页面模板和静态资源均嵌入二进制；`data/` 与 `uploads/` 需要作为持久化目录保留。

