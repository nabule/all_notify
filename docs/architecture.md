# 架构说明

## 总体结构

All Notify 是一个单进程 Go HTTP 服务：

- `cmd/all-notify`：启动入口，加载环境变量，初始化日志、SQLite 和 HTTP Server；Windows 构建时也负责接入 Windows Service Control Manager。
- `internal/server`：HTTP API、Web 页面、请求解析、运行日志读取。
- `internal/store`：SQLite 连接、迁移、配置 CRUD、发送日志写入和裁剪。
- `internal/notify`：Bark、ntfy、SMTP、公告板发送目标。
- `internal/model`：配置、通知请求和日志模型。
- `scripts/start-windows-background.ps1`、`scripts/stop-windows-background.ps1`：Windows 后台启动和停止脚本。
- `scripts/add-windows-service.ps1`、`scripts/remove-windows-service.ps1`、`scripts/install-windows-service.ps1`：Windows x64 服务添加、删除和底层安装更新脚本。
- `scripts/start-linux-background.sh`、`scripts/stop-linux-background.sh`：Linux 后台启动和停止脚本。
- `scripts/add-linux-service.sh`、`scripts/remove-linux-service.sh`：Linux systemd 服务添加和删除脚本。
- `scripts/package-release.ps1`：发布打包脚本，生成发布目录、zip、tar.gz，并携带 `skill/all-notify-usage`。
- `skill/all-notify-usage`：Codex skill 源，用于 All Notify 使用、部署、配置和排障指导。

服务不包含鉴权和用户系统。Web 页面与外部调用方使用同一组 HTTP API。
Web 页面内置“使用说明”页签，并在每个通知入口下根据当前访问域名和入口 key 生成 curl、Python 请求示例，便于调用方直接复制。

## 数据流

1. 调用方请求 `GET/POST /send/{key}`。
2. 服务按 `{key}` 从 SQLite 读取启用的入口和关联目标。
3. 请求解析为标准通知字段：标题、正文、URL、优先级和标签。
4. 每个目标使用独立超时并发发送。
5. 目标失败时按当前全局设置重试；有限重试在当前请求内完成，无限重试把失败目标交给进程内后台任务。
6. 服务汇总每个目标的结果，同步返回 JSON。
7. 发送主日志和目标明细日志写入 SQLite。
8. 按当前设置裁剪发送日志。

## 配置立即生效

配置不使用进程内长期缓存。发送请求每次进入都会读取 SQLite 中当前启用的入口、目标和关联关系，因此通过 Web 或 API 修改后下一次发送立即生效。重试次数和间隔也保存在 SQLite 中，每次重试前重新读取；正在进行的同步重试和后台无限重试都会受到最新设置影响。

## 日志

- 发送日志保存在 SQLite，支持 Web/API 查询和详情查看。
- 运行日志同时写 stdout 和 `/data/logs/app.log`。
- 运行日志按大小轮转。
- 发送日志按保留天数和最大条数自动裁剪。

## 部署

Docker 镜像使用多阶段构建，运行时仅包含 Alpine 和单个 Go 二进制。持久化目录为 `/data`。

Windows x64 单文件程序可通过 `scripts/start-windows-background.ps1` 作为后台进程运行，也可通过 `scripts/add-windows-service.ps1` 注册为 Windows 服务，并用 `scripts/remove-windows-service.ps1` 删除服务。服务脚本默认使用 `dist\all-notify-windows-amd64.exe`，把 `-service-name`、`-addr`、`-data-dir`、日志轮转参数写入服务启动命令；服务收到停止或关机控制事件后会触发 HTTP Server 优雅关闭。Windows 服务模式默认数据目录为 `C:\ProgramData\AllNotify\data`。

Linux x64 单文件程序可通过 `scripts/start-linux-background.sh` 使用 `nohup` 后台运行，也可通过 `scripts/add-linux-service.sh` 注册为 systemd 服务，并用 `scripts/remove-linux-service.sh` 删除服务。systemd 脚本默认服务名为 `all-notify`，运行用户为 `all-notify`，数据目录为 `/var/lib/all-notify`。

发布包由 `scripts/package-release.ps1` 生成。脚本会复制执行文件、文档、部署脚本和 `skill/all-notify-usage`，并把当前 `docs/usage.md` 同步为 skill 的离线参考文件，确保发布包脱离源码仓库后仍可用。
