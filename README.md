# All Notify

All Notify 是一个轻量的聚合通知发送服务。它通过 HTTP API 暴露全部功能，不做鉴权，支持 Bark、ntfy 和 SMTP 邮件，并提供 Web 页面管理配置和查看日志。

## 快速启动

```bash
docker compose up -d --build
```

打开 `http://localhost:8080` 进入 Web 配置页面。默认数据目录为 `./data`，容器内路径为 `/data`。

完整使用流程见 [docs/usage.md](docs/usage.md)。

## HTTP 发送

每个通知入口都有唯一 `key`，发送 URL 为：

```bash
curl "http://localhost:8080/send/server-alert?title=CPU&message=CPU%20usage%20high"
```

POST JSON：

```bash
curl -X POST "http://localhost:8080/send/server-alert" \
  -H "Content-Type: application/json" \
  -d '{"title":"CPU","message":"CPU usage high","url":"https://example.com","tags":["warning"]}'
```

POST 表单：

```bash
curl -X POST "http://localhost:8080/send/server-alert" \
  -d "title=CPU&message=CPU usage high"
```

POST 纯文本：

```bash
curl -X POST "http://localhost:8080/send/server-alert?title=CPU" \
  -H "Content-Type: text/plain" \
  --data "CPU usage high"
```

标准字段：

- `title`：通知标题。
- `message`、`body`、`content`：通知正文，三者任选其一。
- `url`、`click`：点击通知后打开的 URL。
- `priority`、`level`：通知优先级。
- `tags`、`tag`：逗号分隔标签，JSON 也支持数组。

全部目标发送成功返回 `200`；任一目标失败返回 `502`；入口不存在或禁用返回 `404`。

## 配置 API

- `GET /api/routes`：入口列表。
- `POST /api/routes`：创建入口。
- `GET /api/routes/{id}`：入口详情。
- `PUT /api/routes/{id}`：更新入口。
- `DELETE /api/routes/{id}`：删除入口。
- `POST /api/routes/{id}/test`：测试该入口关联的所有启用目标。
- `GET /api/targets`：目标列表。
- `POST /api/targets`：创建目标。
- `GET /api/targets/{id}`：目标详情。
- `PUT /api/targets/{id}`：更新目标。
- `DELETE /api/targets/{id}`：删除目标。
- `POST /api/targets/{id}/test`：只测试该发送目标。
- `GET /api/logs`：发送日志。
- `GET /api/logs/{id}`：发送日志详情。
- `GET /api/runtime-logs`：运行日志。
- `GET /api/settings`、`PUT /api/settings`：日志裁剪设置。

配置保存在 SQLite 中。发送请求每次都会读取当前配置，因此 Web 或 API 修改后立即生效。

Web 页面中，发送目标和通知入口列表都提供“测试”按钮。测试会发送默认测试消息，也会写入发送日志；如果目标不可达，页面会显示失败详情。

## 目标配置示例

Bark：

```json
{
  "server_url": "https://api.day.app",
  "device_key": "your_bark_key",
  "group": "all-notify",
  "sound": "minuet"
}
```

ntfy：

```json
{
  "server_url": "https://ntfy.sh",
  "topic": "your_topic",
  "priority": "default",
  "tags": ["bell"]
}
```

SMTP：

```json
{
  "host": "smtp.example.com",
  "port": 587,
  "security": "starttls",
  "username": "user@example.com",
  "password": "password",
  "from": "user@example.com",
  "to": ["receiver@example.com"],
  "subject_prefix": "[All Notify]"
}
```

`security` 可选 `none`、`starttls`、`tls`。

## 环境变量

- `ALL_NOTIFY_ADDR`：监听地址，默认 `:8080`。
- `ALL_NOTIFY_DATA_DIR`：数据目录，默认 `/data`。
- `ALL_NOTIFY_SEND_TIMEOUT`：单个目标发送超时，默认 `10s`。
- `ALL_NOTIFY_LOG_MAX_BYTES`：运行日志单文件大小，默认 `10485760`。
- `ALL_NOTIFY_LOG_MAX_BACKUPS`：运行日志保留文件数，默认 `5`。

## 测试

本机没有 Go 时可直接使用 Docker：

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -lc "go test ./..."
docker build -t all-notify:test .
```

更完整的测试说明见 [docs/testing.md](docs/testing.md)。
