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

## 2. 环境变量

可在 `docker-compose.yml` 中调整：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `ALL_NOTIFY_ADDR` | `:8080` | HTTP 监听地址 |
| `ALL_NOTIFY_DATA_DIR` | `/data` | 数据和日志目录 |
| `ALL_NOTIFY_SEND_TIMEOUT` | `10s` | 单个发送目标超时时间 |
| `ALL_NOTIFY_LOG_MAX_BYTES` | `10485760` | 单个运行日志文件最大字节数 |
| `ALL_NOTIFY_LOG_MAX_BACKUPS` | `5` | 运行日志轮转保留文件数 |

## 3. Web 页面使用流程

打开 `http://localhost:8080` 后，按以下顺序配置：

1. 进入“发送目标”，新增 Bark、ntfy 或 SMTP 目标。
2. 在目标列表点击“测试”，确认该目标可以收到测试通知。
3. 进入“通知入口”，新增入口并选择一个或多个发送目标。
4. 在入口列表点击“测试”，确认入口关联的所有目标都可以发送。
5. 调用 `/send/{key}` 发送业务通知。
6. 在“发送日志”查看发送结果和目标级错误明细。

服务不做鉴权。请只部署在可信网络或由外部网关控制访问。

## 4. 配置发送目标

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

## 5. 创建通知入口

通知入口通过 `key` 暴露发送 URL。假设入口 key 为 `server-alert`，发送地址就是：

```text
http://localhost:8080/send/server-alert
```

一个入口可以选择多个发送目标，例如同时发送到：

- 2 个 Bark 设备。
- 1 个 ntfy topic。
- 1 组邮件收件人。

配置修改后不需要重启服务，下一次发送请求会读取最新配置。

## 6. 发送通知

### GET

```bash
curl "http://localhost:8080/send/server-alert?title=CPU&message=CPU%20usage%20high"
```

### POST JSON

```bash
curl -X POST "http://localhost:8080/send/server-alert" \
  -H "Content-Type: application/json" \
  -d '{"title":"CPU","message":"CPU usage high","url":"https://example.com","tags":["warning"]}'
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
- `502`：至少一个目标发送失败。
- `404`：入口不存在或已禁用。
- `400`：请求参数无效。

返回体会包含：

- 请求 ID。
- 入口 key。
- 总目标数、成功数、失败数。
- 总耗时。
- 每个目标的状态、耗时、错误和响应。

## 7. 测试功能

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

## 8. 日志查看和裁剪

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

运行日志按文件大小自动轮转，默认单文件 10MB，保留 5 个备份。

## 9. 常见问题

### 保存配置后没有生效

配置保存在 SQLite 中，发送请求每次都会读取最新配置。保存后不需要重启服务。若页面没有刷新，可点击页面上的“刷新”或重新打开页面。

### 测试目标失败

进入“发送日志”，查看日志详情中的 `target_logs`：

- Bark/ntfy 常见问题是服务地址、device key、topic 或网络不可达。
- SMTP 常见问题是端口、安全模式、用户名密码或发件人地址不匹配。

### 发送接口返回 502

`502` 表示至少一个目标发送失败。返回体和日志详情会列出每个目标的结果；部分目标可能已成功收到通知。

### 不想暴露给公网

服务本身不做鉴权。生产使用时建议部署在内网，或在前面加反向代理、VPN、网关白名单等访问控制。
