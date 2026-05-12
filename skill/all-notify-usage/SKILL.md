---
name: all-notify-usage
description: Use when Codex needs to explain, operate, configure, test, troubleshoot, or document the All Notify notification aggregation service; triggers include All Notify usage, all_notify 使用方法, 聚合通知程序怎么用, 配置发送目标, 通知入口, Bark/ntfy/SMTP notification setup, send notification API, Docker deployment, Windows service deployment, Web configuration page, sending logs, and test notification buttons.
---

# All Notify Usage

## Purpose

Use this skill to help a user operate the All Notify service: deploy it, configure Bark/ntfy/SMTP/board targets, create notification routes, send notifications through HTTP, test targets/routes, run it as a Windows service, and inspect logs.

## Source Of Truth

First prefer the project-local documentation when the current workspace is the All Notify repository:

- `docs/usage.md` for full user instructions.
- `README.md` for quick start and API summary.
- `docs/design.md` for API behavior and data model.
- `docs/architecture.md` for deployment and runtime architecture.
- `docs/testing.md` for validation workflows.

If those files are unavailable, read `references/usage.md` bundled with this skill.

## Workflow

1. Identify the user's goal: deploy, configure, send, test, inspect logs, troubleshoot, or install as a Windows service.
2. Load only the relevant section from `docs/usage.md` or `references/usage.md`.
3. Give concrete commands or Web UI steps. Prefer examples the user can run directly.
4. For configuration questions, include the exact JSON shape for the selected target type.
5. For Windows service deployment, prefer `scripts/install-windows-service.ps1` and mention it requires an elevated PowerShell session unless using `-DryRun`.
6. For failures, direct the user to sending log details and `target_logs` first.
7. Remind that the service has no built-in authentication when discussing deployment exposure.

## Common Tasks

- **Start service**: use Docker Compose and check `/healthz`.
- **Run as Windows service**: build or use `all-notify-windows-amd64.exe`, then run `scripts/install-windows-service.ps1 -Restart` from elevated PowerShell.
- **Configure target**: create a Bark, ntfy, SMTP, or board target in the Web page, then use the target Test button.
- **Create route**: create a notification route with a unique `key`, select one or more targets, then use the route Test button.
- **Send notification**: call `GET/POST /send/{key}` with `title` and `message` fields.
- **Debug failure**: inspect Web sending logs or call `GET /api/logs/{id}` and read each `target_logs` item.

## Response Style

Answer in the user's language. Chinese and English are both valid. Keep instructions operational and include exact URLs, curl commands, JSON examples, PowerShell commands, or Web page paths when useful.
