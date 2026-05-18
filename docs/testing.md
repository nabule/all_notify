# 测试说明

## 单元和集成测试

使用 Dockerized Go：

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -lc "go test ./..."
```

测试覆盖：

- GET、JSON、表单、纯文本和 multipart 附件请求解析。
- 配置 API 保存发送目标、保存通知入口、列表查询入口和目标关联。
- Web 首页包含使用说明入口，以及通知入口 curl/Python 示例渲染逻辑。
- Web 概览日志详情会切换到发送日志页签，设置页包含发送重试配置。
- 发送目标测试接口、通知入口测试接口会实际发送并写入日志。
- SQLite 入口列表查询不会因为单连接嵌套查询卡死。
- SQLite 设置包含重试默认值，老数据库迁移会补齐缺失的重试设置。
- Bark、ntfy 和公告板发送器对本地 HTTP test server 的真实请求。
- SMTP 发送器对本地 fake SMTP server 的真实协议交互，包含纯文本邮件和 multipart 附件邮件。
- HTTP 服务配置 API、发送 API 和发送日志落库。
- 发送失败有限重试、重试耗尽、运行中修改重试配置立即生效、无限后台重试停止。
- SQLite 发送日志裁剪。
- Windows/Linux 后台和服务管理脚本需要通过语法或 dry run 验证。
- Windows Service 入口需要通过 Windows x64 编译验证。

## Docker 构建测试

```bash
docker build -t all-notify:test .
docker run --rm -d --name all-notify-test -p 18080:8080 -v "$PWD/tmp-data:/data" all-notify:test
curl http://localhost:18080/healthz
docker rm -f all-notify-test
```

## 跨平台编译验证

发布单文件程序前，至少验证 Linux x64 和 Windows x64 构建：

```bash
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/all-notify-linux-amd64 ./cmd/all-notify
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/all-notify-windows-amd64.exe ./cmd/all-notify
```

Windows 脚本语法检查：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "[scriptblock]::Create((Get-Content .\scripts\install-windows-service.ps1 -Raw)) | Out-Null"
powershell -NoProfile -ExecutionPolicy Bypass -Command "[scriptblock]::Create((Get-Content .\scripts\add-windows-service.ps1 -Raw)) | Out-Null"
powershell -NoProfile -ExecutionPolicy Bypass -Command "[scriptblock]::Create((Get-Content .\scripts\remove-windows-service.ps1 -Raw)) | Out-Null"
powershell -NoProfile -ExecutionPolicy Bypass -Command "[scriptblock]::Create((Get-Content .\scripts\start-windows-background.ps1 -Raw)) | Out-Null"
powershell -NoProfile -ExecutionPolicy Bypass -Command "[scriptblock]::Create((Get-Content .\scripts\stop-windows-background.ps1 -Raw)) | Out-Null"
```

Windows 服务脚本非管理员 dry run 验证：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\add-windows-service.ps1 -DryRun -ExePath .\dist\all-notify-windows-amd64.exe
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\remove-windows-service.ps1 -DryRun
```

Linux 脚本语法和 dry run 验证：

```bash
bash -n scripts/start-linux-background.sh
bash -n scripts/stop-linux-background.sh
bash -n scripts/add-linux-service.sh
bash -n scripts/remove-linux-service.sh
bash scripts/add-linux-service.sh --dry-run --exe /opt/all-notify/all-notify-linux-amd64
bash scripts/remove-linux-service.sh --dry-run
```

发布打包验证：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\package-release.ps1 -Version dev
Select-String -Path .\release\dev\all-notify-dev\MANIFEST.txt -Pattern "skill/all-notify-usage/SKILL.md"
Test-Path .\release\dev\all-notify-dev.zip
Test-Path .\release\dev\all-notify-dev.tar.gz
```

`dist/` 和 `release/` 是本地生成产物，不提交到源码仓库。发布到 GitHub 时只上传 `release/<version>/` 下的 zip、tar.gz 和 `sha256sums.txt` 到 GitHub Releases。

如需 macOS 构建：

```bash
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/all-notify-darwin-arm64 ./cmd/all-notify
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/all-notify-darwin-amd64 ./cmd/all-notify
```

Linux 构建产物可直接做本地冒烟测试：

```bash
./dist/all-notify-linux-amd64 -addr=:18080 -data-dir=./tmp-data
curl http://localhost:18080/healthz
```

## 真实渠道测试

真实 Bark、ntfy、SMTP、公告板测试需要准备自己的设备 key、topic、邮箱账号或公告板 API Token。建议先在 Web 页面创建单个目标并绑定到测试入口，然后调用：

```bash
curl "http://localhost:8080/send/test?title=测试&message=这是一条测试通知"
```

SMTP 附件发送可用 multipart 表单验证。附件只对 SMTP 目标生效，其他目标会忽略附件：

```bash
curl -X POST "http://localhost:8080/send/test" \
  -F "title=测试附件" \
  -F "message=请查看附件" \
  -F "attachments=@./docs/usage.md"
```

发送日志页面应显示每个目标的状态、耗时、错误信息和响应内容。

## 容器端到端测试

完整容器测试建议使用同一个 Docker network 启动：

- 一个 fake HTTP/SMTP 目标容器，模拟 Bark、ntfy 和 SMTP。
- 一个 All Notify 容器。
- 通过 `POST /api/targets` 保存 Bark、ntfy、SMTP 三个目标。
- 通过 `POST /api/routes` 保存一个关联三个目标的通知入口。
- 调用 `POST /send/{key}`，确认返回 `200`、`success_targets=3`，并在 `GET /api/logs` 中看到成功日志。
- 调用 `POST /api/targets/{id}/test` 和 `POST /api/routes/{id}/test`，确认页面测试按钮使用的接口能返回目标级明细。
