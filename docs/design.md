# 设计说明

## API 语义

发送入口：

- `GET /send/{key}`：从 query 读取通知字段。
- `POST /send/{key}`：支持 JSON、`application/x-www-form-urlencoded`、`text/plain` 和 `multipart/form-data`。

标准字段：

- `title`
- `message` / `body` / `content`
- `url` / `click`
- `priority` / `level`
- `tags` / `tag`
- `attachment` / `attachments`：仅 `multipart/form-data` 可用，可重复上传多个文件。

附件语义：

- 单个附件最大 10MB，单次请求最大 25MB。
- 仅 SMTP 目标使用附件；Bark、ntfy 和公告板忽略附件。
- 发送日志只记录附件文件名、Content-Type 和大小摘要，不保存附件内容。

返回语义：

- `200`：全部目标发送成功。
- `502`：按当前重试设置同步重试后，至少一个目标仍发送失败。
- `404`：入口不存在或已禁用。
- `400`：请求字段无效。

返回体包含请求 ID、入口 key、整体状态、目标数量、成功/失败数量、耗时，以及每个目标的发送结果。
列表类 API 在没有数据时返回空数组 `[]`，便于 Web 页面和外部调用方按数组处理。

发送失败重试：

- 全局设置 `retry_max_retries` 表示初次失败后的额外重试次数，默认 `3`。
- `retry_max_retries = 0` 表示不重试，正数表示最多重试 N 次，`-1` 表示失败目标进入进程内后台无限重试。
- `retry_interval_seconds` 表示重试间隔秒数，默认 `5`。
- 每次重试前都会重新读取设置。设置调小会让正在进行的重试提前停止，设置调大会允许后续更多重试，间隔修改会影响下一次等待。
- 无限后台重试不阻塞发送接口，也不跨服务重启恢复。

配置测试接口：

- `POST /api/targets/{id}/test`：只对该发送目标发送测试通知。
- `POST /api/routes/{id}/test`：通过该通知入口关联的所有启用目标发送测试通知。
- 测试接口可传普通通知 JSON；不传请求体时使用默认测试标题和正文。
- 测试结果写入发送日志，失败时仍返回目标级错误明细。

## Web 页面行为

- 顶部提供“使用说明”页签，展示配置流程、标准字段、返回状态和部署暴露提醒。
- “通知入口”列表按每个入口的 `key` 和当前 `window.location.origin` 自动生成发送 URL。
- 每个入口展示 curl 和 Python 两类请求示例，示例使用 `GET /send/{key}` 和 `POST /send/{key}` JSON 发送。
- 示例仅用于调用发送入口，不改变配置 API 或发送 API 的语义。

## 数据模型

`routes` 保存通知入口：

- `key`：URL 中使用的唯一配置标识。
- `name`：页面显示名称。
- `default_title`：请求未传标题时使用。
- `enabled`：禁用后发送接口返回 `404`。

`targets` 保存发送目标：

- `type`：`bark`、`ntfy`、`smtp`、`board`。
- `config`：目标配置 JSON。
- `enabled`：禁用后发送时忽略。

`route_targets` 保存入口和目标的多对多关系。

`send_logs` 和 `target_send_logs` 保存发送主日志和每个目标的明细结果。

## 发送渠道

Bark 使用 JSON POST。单设备发送到 `/{device_key}`，多设备发送到 `/push` 并传 `device_keys`。

ntfy 使用 HTTP POST 到 `{server_url}/{topic}`，正文为纯文本，标题、优先级、标签和点击链接通过请求头传递。

SMTP 使用标准 SMTP 协议，支持 `none`、`starttls`、`tls` 三种安全模式。无附件时发送 UTF-8 纯文本邮件；有附件时发送 `multipart/mixed` 邮件，正文和附件内容均使用 base64 传输编码。`to`、`cc`、`bcc` 都支持多个收件人，`bcc` 只参与投递，不写入邮件头。

公告板使用 JSON POST 到 `{server_url}/api/update/{board_id}`，鉴权头为 `Authorization: Bearer {api_token}`。目标配置中的 `mode` 控制 `action` 字段：`append` 表示追加公告，`new` 表示覆盖当前频道并写入新公告；未配置时默认 `append`。

## 稳定性策略

- SQLite 使用 WAL、busy timeout 和外键。
- 单个目标默认 10 秒超时。
- 目标并发发送，失败后按全局设置自动重试。
- JSON、表单和纯文本请求体限制为 1MB；multipart 附件请求限制为 25MB，单附件限制为 10MB。
- 运行日志按大小轮转。
- Windows 服务模式接入 Windows Service Control Manager，停止或关机事件会触发与控制台模式一致的优雅关闭流程。

## 部署脚本

后台启动脚本只负责以本机后台进程方式启动单文件程序并写入 PID 文件，不改变 HTTP API。Windows 默认使用 `data\all-notify.pid`，Linux 默认使用 `data/all-notify.pid`；停止脚本通过 PID 文件结束进程。

Windows x64 服务脚本只负责本机服务注册和启动参数管理，不改变 HTTP API。`add-windows-service.ps1` 和 `remove-windows-service.ps1` 是面向用户的添加/删除入口，内部复用 `install-windows-service.ps1`。脚本默认查找 `dist\all-notify-windows-amd64.exe`，服务数据目录使用 `C:\ProgramData\AllNotify\data`，并支持通过参数覆盖服务名、监听地址、数据目录、发送超时和运行日志轮转设置。

Linux systemd 脚本只负责生成和删除 `/etc/systemd/system/{service}.service`，不改变 HTTP API。`add-linux-service.sh` 默认使用服务名 `all-notify`、运行用户 `all-notify`、数据目录 `/var/lib/all-notify`，并支持通过参数覆盖服务名、可执行文件、监听地址、数据目录、发送超时和运行日志轮转设置。

发布打包脚本只负责构建和归档，不改变运行时行为。发布包固定包含 `skill/all-notify-usage`，其中 `references/usage.md` 在打包时由当前 `docs/usage.md` 覆盖生成，避免 skill 内离线说明与项目使用文档脱节。
