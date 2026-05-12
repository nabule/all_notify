# 使用说明

本文面向部署和使用 All Notify 的人员，说明如何启动服务、配置发送目标、创建通知入口、发送通知、测试配置和查看日志。

## 1. 启动服务

推荐使用 Docker Compose：

```bash
docker compose up -d --build
```

默认访问地址：

- Web 配置页面：`http://localhost:8080`
- 健康检查：`http://localhost:8080/healthz`

默认持久化目录：

- 宿主机：`./data`
- 容器内：`/data`

`/data` 中会保存：

- `all_notify.db`：SQLite 数据库，包含配置和发送日志。
- `logs/app.log`：运行日志。

## 2. 编译和运行

### 2.1 前置条件

本地编译需要 Go 1.23 或更新版本：

```bash
go version
```

服务是单个 Go HTTP 程序，入口为 `./cmd/all-notify`。默认监听 `:8080`，默认数据目录为 `/data`；单文件运行时推荐使用启动参数显式传入配置。

### 2.2 本机开发运行

Linux/macOS/WSL：

```bash
go run ./cmd/all-notify -- -addr=:8080 -data-dir=./data -send-timeout=10s
```

Windows PowerShell：

```powershell
go run .\cmd\all-notify -- -addr=:8080 -data-dir=.\data -send-timeout=10s
```

启动后访问：

- Web 配置页面：`http://localhost:8080`
- 健康检查：`http://localhost:8080/healthz`

### 2.3 Linux x64 单文件

在 Linux/macOS/WSL shell 中编译：

```bash
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/all-notify-linux-amd64 ./cmd/all-notify
```

运行：

```bash
chmod +x dist/all-notify-linux-amd64
./dist/all-notify-linux-amd64 -addr=:8080 -data-dir=./data -send-timeout=10s -log-max-bytes=10485760 -log-max-backups=5
```

### 2.4 Windows x64 单文件

在 Windows PowerShell 中编译：

```powershell
New-Item -ItemType Directory -Force dist | Out-Null
$env:CGO_ENABLED="0"
$env:GOOS="windows"
$env:GOARCH="amd64"
go build -trimpath -ldflags="-s -w" -o dist/all-notify-windows-amd64.exe ./cmd/all-notify
```

运行：

```powershell
.\dist\all-notify-windows-amd64.exe -addr=:8080 -data-dir=.\data -send-timeout=10s -log-max-bytes=10485760 -log-max-backups=5
```

安装为 Windows 服务：

```powershell
$script = (Resolve-Path .\scripts\install-windows-service.ps1).Path
Start-Process powershell -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$script`" -Restart"
```

脚本默认服务名为 `AllNotify`，默认查找 `dist\all-notify-windows-amd64.exe`，默认数据目录为 `C:\ProgramData\AllNotify\data`，默认监听 `:8080`。脚本会把 `-service-name` 写入服务启动参数，服务名自定义后也可正常响应 Windows Service Control Manager。如需自定义参数：

```powershell
$script = (Resolve-Path .\scripts\install-windows-service.ps1).Path
$exe = (Resolve-Path .\dist\all-notify-windows-amd64.exe).Path
Start-Process powershell -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$script`" -ExePath `"$exe`" -ServiceName AllNotify -Addr :18888 -DataDir C:\all-notify\data -Restart"
```

卸载服务：

```powershell
$script = (Resolve-Path .\scripts\install-windows-service.ps1).Path
Start-Process powershell -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$script`" -Uninstall"
```

### 2.5 macOS 单文件

Apple Silicon：

```bash
mkdir -p dist
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/all-notify-darwin-arm64 ./cmd/all-notify
./dist/all-notify-darwin-arm64 -addr=:8080 -data-dir=./data -send-timeout=10s
```

Intel Mac：

```bash
mkdir -p dist
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/all-notify-darwin-amd64 ./cmd/all-notify
./dist/all-notify-darwin-amd64 -addr=:8080 -data-dir=./data -send-timeout=10s
```

### 2.6 Docker 运行

直接使用 Docker：

```bash
docker build -t all-notify:local .
docker run --rm -p 8080:8080 -v "$PWD/data:/data" all-notify:local -addr=:8080 -data-dir=/data -send-timeout=10s
```

Windows PowerShell 使用 Docker 挂载本地数据目录：

```powershell
docker build -t all-notify:local .
docker run --rm -p 8080:8080 -v "${PWD}\data:/data" all-notify:local -addr=:8080 -data-dir=/data -send-timeout=10s
```

### 2.7 发布打包

在 Windows PowerShell 中生成发布目录、zip 和 tar.gz：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\package-release.ps1 -Version dev
```

打包脚本默认编译 Linux x64 和 Windows x64 单文件，并把以下内容放入发布包：

- `bin/`：执行文件和 SHA256 校验文件。
- `docs/`：架构、设计、测试和使用说明。
- `scripts/`：Windows 服务安装脚本和发布打包脚本。
- `skill/all-notify-usage/`：Codex skill，可用于使用、部署、配置和排障指导。

生成的 `skill/all-notify-usage/references/usage.md` 会同步当前 `docs/usage.md`，因此 skill 离开源码仓库后也能提供完整使用说明。

## 3. 启动参数

单文件程序推荐使用命令行参数启动：

```bash
./all-notify -addr=:8080 -data-dir=./data -send-timeout=10s -log-max-bytes=10485760 -log-max-backups=5
```

`docker-compose.yml` 也通过 `command` 向容器内的单文件程序传参。参数优先级高于同名环境变量，环境变量仅作为兼容默认值。

| 参数 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `-addr` | `ALL_NOTIFY_ADDR` | `:8080` | HTTP 监听地址 |
| `-data-dir` | `ALL_NOTIFY_DATA_DIR` | `/data` | 数据和日志目录 |
| `-send-timeout` | `ALL_NOTIFY_SEND_TIMEOUT` | `10s` | 单个发送目标超时时间 |
| `-log-max-bytes` | `ALL_NOTIFY_LOG_MAX_BYTES` | `10485760` | 单个运行日志文件最大字节数 |
| `-log-max-backups` | `ALL_NOTIFY_LOG_MAX_BACKUPS` | `5` | 运行日志轮转保留文件数 |

## 4. Web 页面使用流程

打开 `http://localhost:8080` 后，按以下顺序配置：

1. 进入“发送目标”，新增 Bark、ntfy、SMTP 或公告板目标。
2. 在目标列表点击“测试”，确认该目标可以收到测试通知。
3. 进入“通知入口”，新增入口并选择一个或多个发送目标。
4. 在入口列表查看该入口对应的 curl 和 Python 请求示例。
5. 在入口列表点击“测试”，确认入口关联的所有目标都可以发送。
6. 调用 `/send/{key}` 发送业务通知。
7. 在“发送日志”查看发送结果和目标级错误明细。

页面顶部的“使用说明”页签提供配置流程、标准字段和返回状态说明，适合在部署后直接给调用方查看。

服务不做鉴权。请只部署在可信网络或由外部网关控制访问。

## 5. 配置发送目标

### Bark

Web 页面选择类型 `bark`，配置示例：

```json
{
  "server_url": "https://api.day.app",
  "device_key": "your_bark_key",
  "group": "all-notify",
  "sound": "minuet",
  "icon": "https://example.com/icon.png",
  "level": "active"
}
```

字段说明：

- `server_url`：Bark 服务地址，默认可用 `https://api.day.app`。
- `device_key`：单个 Bark 设备 key。
- `device_keys`：多个 Bark 设备 key 数组。使用该字段时会调用 Bark `/push` 接口。
- `group`、`sound`、`icon`、`level`：Bark 可选参数。

### ntfy

Web 页面选择类型 `ntfy`，配置示例：

```json
{
  "server_url": "https://ntfy.sh",
  "topic": "your_topic",
  "priority": "default",
  "tags": ["bell"]
}
```

带认证的 ntfy 示例：

```json
{
  "server_url": "https://ntfy.example.com",
  "topic": "ops-alert",
  "token": "tk_xxx"
}
```

或使用 basic auth：

```json
{
  "server_url": "https://ntfy.example.com",
  "topic": "ops-alert",
  "username": "user",
  "password": "password"
}
```

### SMTP 邮件

Web 页面选择类型 `smtp`，配置示例：

```json
{
  "host": "smtp.example.com",
  "port": 587,
  "security": "starttls",
  "username": "user@example.com",
  "password": "password",
  "from": "user@example.com",
  "to": ["receiver@example.com"],
  "cc": [],
  "bcc": [],
  "subject_prefix": "[All Notify]"
}
```

`security` 支持：

- `none`：普通 SMTP。
- `starttls`：连接后升级 TLS，常用于 587 端口。
- `tls`：直接 TLS 连接，常用于 465 端口。

### 公告板

Web 页面选择类型 `board`，配置示例：

```json
{
  "server_url": "https://board.12342345.xyz",
  "board_id": "hr",
  "api_token": "admin123",
  "mode": "append"
}
```

字段说明：

- `server_url`：公告板服务地址，例如 `https://board.12342345.xyz`。
- `board_id`：公告板频道 ID，例如 `hr` 或 `tech`。
- `api_token`：公告板接口 Bearer Token。
- `mode`：写入模式，`append` 表示追加公告，`new` 表示覆盖当前频道并新建一条公告；未填时默认 `append`。

公告板发送时会调用 `POST {server_url}/api/update/{board_id}`，请求体为 `{"action": mode, "content": message}`。如果发送请求带有 `url` 或 `click` 字段，服务会把 URL 追加到公告内容末尾。

## 6. 创建通知入口

通知入口通过 `key` 暴露发送 URL。假设入口 key 为 `server-alert`，发送地址就是：

```text
http://localhost:8080/send/server-alert
```

一个入口可以选择多个发送目标，例如同时发送到：

- 2 个 Bark 设备。
- 1 个 ntfy topic。
- 1 组邮件收件人。
- 1 个公告板频道。

配置修改后不需要重启服务，下一次发送请求会读取最新配置。

Web 页面会在每个入口下面按当前访问域名和入口 key 自动生成请求示例，包含 curl 和 Python 两类示例。复制前请确认页面访问地址就是业务调用方可访问的服务地址。

## 7. 发送通知

下面继续以入口 key `server-alert` 为例。

### curl GET

```bash
curl "http://localhost:8080/send/server-alert?title=CPU&message=CPU%20usage%20high"
```

### curl POST JSON

```bash
curl -X POST "http://localhost:8080/send/server-alert" \
  -H "Content-Type: application/json" \
  -d '{"title":"CPU","message":"CPU usage high","url":"https://example.com","tags":["warning"]}'
```

### Python POST JSON

```python
import json
import urllib.request

url = "http://localhost:8080/send/server-alert"
payload = {
    "title": "CPU",
    "message": "CPU usage high",
    "url": "https://example.com",
    "tags": ["warning"],
}

req = urllib.request.Request(
    url,
    data=json.dumps(payload).encode("utf-8"),
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(req, timeout=10) as resp:
    print(resp.status, resp.read().decode("utf-8"))
```

### POST 表单

```bash
curl -X POST "http://localhost:8080/send/server-alert" \
  -d "title=CPU&message=CPU usage high"
```

### POST 纯文本

```bash
curl -X POST "http://localhost:8080/send/server-alert?title=CPU" \
  -H "Content-Type: text/plain" \
  --data "CPU usage high"
```

### 标准字段

| 字段 | 说明 |
| --- | --- |
| `title` | 通知标题 |
| `message` / `body` / `content` | 通知正文 |
| `url` / `click` | 点击通知后打开的 URL |
| `priority` / `level` | 优先级 |
| `tags` / `tag` | 标签，GET/表单中用逗号分隔，JSON 中可用数组 |

### 返回状态

- `200`：全部目标发送成功。
- `502`：按当前重试设置同步重试后，至少一个目标仍发送失败。
- `404`：入口不存在或已禁用。
- `400`：请求参数无效。

返回体会包含：

- 请求 ID。
- 入口 key。
- 总目标数、成功数、失败数。
- 总耗时。
- 每个目标的状态、耗时、错误和响应。

发送失败会按“设置”页面中的全局重试配置自动重试。重试次数是初次失败后的额外重试次数：`0` 表示不重试，正数表示最多重试 N 次，`-1` 表示失败目标进入进程内后台无限重试。重试间隔单位为秒。服务每次重试前都会重新读取设置，因此保存后会立即影响正在进行的重试。

## 8. 测试功能

Web 页面提供两类测试：

- “发送目标”列表中的“测试”：只测试单个目标。
- “通知入口”列表中的“测试”：测试入口关联的所有启用目标。

测试会发送默认测试消息，也可以通过 API 传入自定义测试内容：

```bash
curl -X POST "http://localhost:8080/api/targets/1/test" \
  -H "Content-Type: application/json" \
  -d '{"title":"目标测试","message":"这是一条目标测试通知"}'
```

```bash
curl -X POST "http://localhost:8080/api/routes/1/test" \
  -H "Content-Type: application/json" \
  -d '{"title":"入口测试","message":"这是一条入口测试通知"}'
```

测试结果会写入发送日志。即使测试失败，也可以在日志详情中看到目标级错误，例如连接失败、SMTP 认证失败或 HTTP 状态码错误。

## 9. 日志查看和裁剪

Web 页面“发送日志”可查看：

- 发送时间。
- 入口 key。
- 成功/失败状态。
- 成功目标数和总目标数。
- 每个目标的错误和响应。

Web 页面“运行日志”可查看服务运行日志。

“设置”页面可以调整发送日志裁剪：

- 保留天数，默认 30 天。
- 最大条数，默认 100000 条。

也可以调整发送失败重试：

- 重试次数，默认 3 次；`0` 表示不重试，`-1` 表示无限后台重试。
- 重试间隔秒数，默认 5 秒。

运行日志按文件大小自动轮转，默认单文件 10MB，保留 5 个备份。

## 10. 常见问题

### 保存配置后没有生效

配置保存在 SQLite 中，发送请求每次都会读取最新配置。重试设置每次重试前也会重新读取，因此修改重试次数或间隔会立即影响正在进行的重试。保存后不需要重启服务。若页面没有刷新，可点击页面上的“刷新”或重新打开页面。

### 测试目标失败

进入“发送日志”，查看日志详情中的 `target_logs`：

- Bark/ntfy 常见问题是服务地址、device key、topic 或网络不可达。
- SMTP 常见问题是端口、安全模式、用户名密码或发件人地址不匹配。

### 发送接口返回 502

`502` 表示同步重试后至少一个目标仍发送失败。返回体和日志详情会列出每个目标的结果；部分目标可能已成功收到通知。若重试次数设置为 `-1`，失败目标会在返回后继续由当前进程后台重试，直到成功、设置改为有限/不重试或服务退出。

### 不想暴露给公网

服务本身不做鉴权。生产使用时建议部署在内网，或在前面加反向代理、VPN、网关白名单等访问控制。
