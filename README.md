# All Notify

All Notify 是一个轻量的聚合通知发送服务。它通过 HTTP API 暴露全部功能，不做鉴权，支持 Bark、ntfy、SMTP 邮件和公告板，并提供 Web 页面管理配置和查看日志。

## 快速启动

```bash
docker compose up -d --build
```

打开 `http://localhost:8080` 进入 Web 配置页面。默认数据目录为 `./data`，容器内路径为 `/data`。

完整使用流程见 [docs/usage.md](docs/usage.md)。
Web 页面顶部有“使用说明”入口，“通知入口”列表会按每个入口的 `key` 自动生成 curl 和 Python 请求示例。

## 编译和运行

需要 Go 1.23 或更新版本。本机运行：

```bash
go run ./cmd/all-notify -- -addr=:8080 -data-dir=./data -send-timeout=10s
```

Linux x64 单文件：

```bash
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/all-notify-linux-amd64 ./cmd/all-notify
./dist/all-notify-linux-amd64 -addr=:8080 -data-dir=./data -send-timeout=10s -log-max-bytes=10485760 -log-max-backups=5
```

Windows x64 单文件：

```powershell
New-Item -ItemType Directory -Force dist | Out-Null
$env:CGO_ENABLED="0"; $env:GOOS="windows"; $env:GOARCH="amd64"
go build -trimpath -ldflags="-s -w" -o dist/all-notify-windows-amd64.exe ./cmd/all-notify
.\dist\all-notify-windows-amd64.exe -addr=:8080 -data-dir=.\data -send-timeout=10s -log-max-bytes=10485760 -log-max-backups=5
```

Windows 后台启动/停止：

```powershell
.\scripts\start-windows-background.ps1 -ExePath .\dist\all-notify-windows-amd64.exe
.\scripts\stop-windows-background.ps1
```

Windows 服务添加/删除：

```powershell
$script = (Resolve-Path .\scripts\add-windows-service.ps1).Path
Start-Process powershell -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$script`" -Restart"

$script = (Resolve-Path .\scripts\remove-windows-service.ps1).Path
Start-Process powershell -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$script`""
```

脚本默认服务名为 `AllNotify`，默认查找 `dist\all-notify-windows-amd64.exe`，默认数据目录为 `C:\ProgramData\AllNotify\data`。

Linux 后台启动/停止和 systemd 服务：

```bash
./scripts/start-linux-background.sh --exe ./dist/all-notify-linux-amd64
./scripts/stop-linux-background.sh

sudo ./scripts/add-linux-service.sh --exe /opt/all-notify/all-notify-linux-amd64 --restart
sudo ./scripts/remove-linux-service.sh
```

Docker 运行：

```bash
docker build -t all-notify:local .
docker run --rm -p 8080:8080 -v "$PWD/data:/data" all-notify:local -addr=:8080 -data-dir=/data -send-timeout=10s
```

发布打包：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\package-release.ps1 -Version dev
```

发布包会包含 `bin/`、`docs/`、`scripts/` 和 `skill/all-notify-usage/`。`scripts/` 内含 Windows/Linux 后台启动、停止、服务添加和服务删除脚本；skill 可用于 Codex 的 All Notify 使用、部署、配置和排障指导。

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

POST multipart 附件：

```bash
curl -X POST "http://localhost:8080/send/server-alert" \
  -F "title=日报" \
  -F "message=见附件" \
  -F "attachments=@./report.pdf" \
  -F "attachments=@./metrics.csv"
```

附件只对 SMTP 发送目标生效，Bark、ntfy 和公告板会忽略附件。单个附件最大 10MB，单次请求最大 25MB。

标准字段：

- `title`：通知标题。
- `message`、`body`、`content`：通知正文，三者任选其一。
- `url`、`click`：点击通知后打开的 URL。
- `priority`、`level`：通知优先级。
- `tags`、`tag`：逗号分隔标签，JSON 也支持数组。
- `attachment`、`attachments`：`multipart/form-data` 附件字段，可重复上传多个文件。

全部目标发送成功返回 `200`；同步重试后仍有任一目标失败返回 `502`；入口不存在或禁用返回 `404`。

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
- `GET /api/settings`、`PUT /api/settings`：日志裁剪和发送重试设置。

配置保存在 SQLite 中。发送请求每次都会读取当前配置，因此 Web 或 API 修改后立即生效。发送失败会按全局设置自动重试，`retry_max_retries` 为初次失败后的额外重试次数：`0` 表示不重试，正数表示最多重试 N 次，`-1` 表示失败目标转入进程内后台无限重试。重试任务每次重试前都会重新读取设置，因此修改重试次数或间隔会立即影响正在进行的重试。

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
  "to": ["receiver@example.com", "ops@example.com"],
  "cc": ["manager@example.com"],
  "bcc": [],
  "subject_prefix": "[All Notify]"
}
```

`security` 可选 `none`、`starttls`、`tls`。

公告板：

```json
{
  "server_url": "https://board.12342345.xyz",
  "board_id": "hr",
  "api_token": "admin123",
  "mode": "append"
}
```

## 发布

源码仓库会忽略 `dist/` 和 `release/` 本地生成产物。发布版本时运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\package-release.ps1 -Version v1.2.0
```

然后把 `release/v1.2.0/` 下的 zip、tar.gz 和 `sha256sums.txt` 上传到 GitHub Releases。

`mode` 可选 `append` 或 `new`：`append` 会追加公告，`new` 会覆盖当前频道并写入一条新公告。未配置时默认 `append`。

## 启动参数

单文件程序推荐使用命令行参数启动：

```bash
./all-notify -addr=:8080 -data-dir=./data -send-timeout=10s -log-max-bytes=10485760 -log-max-backups=5
```

参数优先级高于同名环境变量，环境变量仅作为兼容默认值。

| 参数 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `-addr` | `ALL_NOTIFY_ADDR` | `:8080` | HTTP 监听地址 |
| `-data-dir` | `ALL_NOTIFY_DATA_DIR` | `/data` | 数据和日志目录 |
| `-send-timeout` | `ALL_NOTIFY_SEND_TIMEOUT` | `10s` | 单个发送目标超时时间 |
| `-log-max-bytes` | `ALL_NOTIFY_LOG_MAX_BYTES` | `10485760` | 单个运行日志文件最大字节数 |
| `-log-max-backups` | `ALL_NOTIFY_LOG_MAX_BACKUPS` | `5` | 运行日志轮转保留文件数 |


## 测试

本机没有 Go 时可直接使用 Docker：

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -lc "go test ./..."
docker build -t all-notify:test .
```

更完整的测试说明见 [docs/testing.md](docs/testing.md)。
