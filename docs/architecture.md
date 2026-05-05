# 架构说明

## 总体结构

All Notify 是一个单进程 Go HTTP 服务：

- `cmd/all-notify`：启动入口，加载环境变量，初始化日志、SQLite 和 HTTP Server。
- `internal/server`：HTTP API、Web 页面、请求解析、运行日志读取。
- `internal/store`：SQLite 连接、迁移、配置 CRUD、发送日志写入和裁剪。
- `internal/notify`：Bark、ntfy、SMTP 三类发送目标。
- `internal/model`：配置、通知请求和日志模型。

服务不包含鉴权和用户系统。Web 页面与外部调用方使用同一组 HTTP API。

## 数据流

1. 调用方请求 `GET/POST /send/{key}`。
2. 服务按 `{key}` 从 SQLite 读取启用的入口和关联目标。
3. 请求解析为标准通知字段：标题、正文、URL、优先级和标签。
4. 每个目标使用独立超时并发发送。
5. 服务汇总每个目标的结果，同步返回 JSON。
6. 发送主日志和目标明细日志写入 SQLite。
7. 按当前设置裁剪发送日志。

## 配置立即生效

配置不使用进程内长期缓存。发送请求每次进入都会读取 SQLite 中当前启用的入口、目标和关联关系，因此通过 Web 或 API 修改后下一次发送立即生效。

## 日志

- 发送日志保存在 SQLite，支持 Web/API 查询和详情查看。
- 运行日志同时写 stdout 和 `/data/logs/app.log`。
- 运行日志按大小轮转。
- 发送日志按保留天数和最大条数自动裁剪。

## 部署

Docker 镜像使用多阶段构建，运行时仅包含 Alpine 和单个 Go 二进制。持久化目录为 `/data`。
