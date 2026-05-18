#Requires -Version 5.1

[CmdletBinding()]
param(
    [string]$PidFile = "",
    [int]$TimeoutSeconds = 30,
    [switch]$Force
)

$ErrorActionPreference = "Stop"

function Resolve-FullPath([string]$Path) {
    if ([IO.Path]::IsPathRooted($Path)) {
        return [IO.Path]::GetFullPath($Path)
    }
    return [IO.Path]::GetFullPath((Join-Path (Get-Location) $Path))
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($PidFile)) {
    $PidFile = Join-Path (Split-Path -Parent $scriptDir) "data\all-notify.pid"
}
$PidFile = Resolve-FullPath $PidFile

if (-not (Test-Path -LiteralPath $PidFile -PathType Leaf)) {
    Write-Host "PID 文件不存在，后台进程可能未运行: $PidFile"
    exit 0
}

$pidText = (Get-Content -LiteralPath $PidFile -Raw).Trim()
if ($pidText -notmatch '^\d+$') {
    Remove-Item -LiteralPath $PidFile -Force
    throw "PID 文件内容无效，已删除: $PidFile"
}

$process = Get-Process -Id ([int]$pidText) -ErrorAction SilentlyContinue
if (-not $process) {
    Remove-Item -LiteralPath $PidFile -Force
    Write-Host "进程不存在，已删除 PID 文件: $PidFile"
    exit 0
}

$sentClose = $false
if ($process.MainWindowHandle -ne 0) {
    $sentClose = $process.CloseMainWindow()
}
if (-not $sentClose) {
    Stop-Process -Id ([int]$pidText)
}
$deadline = (Get-Date).AddSeconds($TimeoutSeconds)
while (-not $process.HasExited -and (Get-Date) -lt $deadline) {
    Start-Sleep -Milliseconds 500
    $process.Refresh()
}

if (-not $process.HasExited) {
    if (-not $Force) {
        throw "进程未在 $TimeoutSeconds 秒内退出。可追加 -Force 强制停止。PID: $pidText"
    }
    Stop-Process -Id ([int]$pidText) -Force
}

Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
Write-Host "All Notify 后台进程已停止，PID: $pidText"
