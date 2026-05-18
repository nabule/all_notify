#Requires -Version 5.1

[CmdletBinding()]
param(
    [string]$ExePath = "",
    [string]$DataDir = "",
    [string]$Addr = ":8080",
    [string]$SendTimeout = "10s",
    [int64]$LogMaxBytes = 10485760,
    [int]$LogMaxBackups = 5,
    [string]$PidFile = "",
    [string]$StdoutLog = "",
    [string]$StderrLog = ""
)

$ErrorActionPreference = "Stop"

function Resolve-FullPath([string]$Path) {
    if ([IO.Path]::IsPathRooted($Path)) {
        return [IO.Path]::GetFullPath($Path)
    }
    return [IO.Path]::GetFullPath((Join-Path (Get-Location) $Path))
}

function Resolve-DefaultExePath([string]$ScriptDir) {
    $candidates = @(
        (Join-Path $ScriptDir "all-notify-windows-amd64.exe"),
        (Join-Path $ScriptDir "..\dist\all-notify-windows-amd64.exe"),
        (Join-Path $ScriptDir "..\bin\all-notify-windows-amd64.exe")
    )
    foreach ($candidate in $candidates) {
        $fullPath = [IO.Path]::GetFullPath($candidate)
        if (Test-Path -LiteralPath $fullPath -PathType Leaf) {
            return $fullPath
        }
    }
    return [IO.Path]::GetFullPath($candidates[0])
}

function Ensure-ParentDirectory([string]$Path) {
    $parent = Split-Path -Parent $Path
    if (-not [string]::IsNullOrWhiteSpace($parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($ExePath)) {
    $ExePath = Resolve-DefaultExePath $scriptDir
}
if ([string]::IsNullOrWhiteSpace($DataDir)) {
    $DataDir = Join-Path (Split-Path -Parent $scriptDir) "data"
}
$ExePath = Resolve-FullPath $ExePath
$DataDir = Resolve-FullPath $DataDir
if ([string]::IsNullOrWhiteSpace($PidFile)) {
    $PidFile = Join-Path $DataDir "all-notify.pid"
}
if ([string]::IsNullOrWhiteSpace($StdoutLog)) {
    $StdoutLog = Join-Path $DataDir "logs\stdout.log"
}
if ([string]::IsNullOrWhiteSpace($StderrLog)) {
    $StderrLog = Join-Path $DataDir "logs\stderr.log"
}
$PidFile = Resolve-FullPath $PidFile
$StdoutLog = Resolve-FullPath $StdoutLog
$StderrLog = Resolve-FullPath $StderrLog

if (-not (Test-Path -LiteralPath $ExePath -PathType Leaf)) {
    throw "找不到 All Notify Windows 可执行文件: $ExePath"
}
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
Ensure-ParentDirectory $PidFile
Ensure-ParentDirectory $StdoutLog
Ensure-ParentDirectory $StderrLog

if (Test-Path -LiteralPath $PidFile -PathType Leaf) {
    $existingPid = (Get-Content -LiteralPath $PidFile -Raw).Trim()
    if ($existingPid -match '^\d+$') {
        $existing = Get-Process -Id ([int]$existingPid) -ErrorAction SilentlyContinue
        if ($existing) {
            Write-Host "All Notify 已在后台运行，PID: $existingPid"
            exit 0
        }
    }
}

$args = @(
    "-addr=$Addr",
    "-data-dir=$DataDir",
    "-send-timeout=$SendTimeout",
    "-log-max-bytes=$LogMaxBytes",
    "-log-max-backups=$LogMaxBackups"
)
$process = Start-Process -FilePath $ExePath -ArgumentList $args -WorkingDirectory (Split-Path -Parent $ExePath) -RedirectStandardOutput $StdoutLog -RedirectStandardError $StderrLog -WindowStyle Hidden -PassThru
Set-Content -LiteralPath $PidFile -Value $process.Id -Encoding ASCII

Write-Host "All Notify 已后台启动，PID: $($process.Id)"
Write-Host "监听地址: $Addr"
Write-Host "数据目录: $DataDir"
Write-Host "PID 文件: $PidFile"
Write-Host "标准输出日志: $StdoutLog"
Write-Host "标准错误日志: $StderrLog"
