#Requires -Version 5.1

[CmdletBinding()]
param(
    [string]$ServiceName = "AllNotify",
    [string]$DisplayName = "All Notify",
    [string]$Description = "All Notify HTTP notification aggregation service",
    [string]$ExePath = "",
    [string]$DataDir = "",
    [string]$Addr = ":8080",
    [string]$SendTimeout = "10s",
    [int64]$LogMaxBytes = 10485760,
    [int]$LogMaxBackups = 5,
    [switch]$Restart,
    [switch]$Uninstall,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"

function Assert-Admin {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "请以管理员身份运行 PowerShell。"
    }
}

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

function Quote-Arg([string]$Value) {
    if ($Value.Contains('"')) {
        throw "服务启动参数不能包含双引号: $Value"
    }
    if ($Value -match '[\s"]') {
        return '"' + $Value + '"'
    }
    return $Value
}

function New-FlagArg([string]$Name, [string]$Value) {
    return "-$Name=$(Quote-Arg $Value)"
}

function Invoke-Sc([string[]]$Arguments) {
    if ($DryRun) {
        Write-Host "DRY RUN: sc.exe $($Arguments -join ' ')"
        return
    }

    & sc.exe @Arguments | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "sc.exe 执行失败，退出码: $LASTEXITCODE"
    }
}

function Stop-ServiceIfRunning([string]$Name) {
    $service = Get-Service -Name $Name -ErrorAction SilentlyContinue
    if (-not $service) {
        return
    }
    if ($service.Status -ne "Stopped") {
        if ($DryRun) {
            Write-Host "DRY RUN: Stop-Service -Name $Name -Force"
            return
        }
        Stop-Service -Name $Name -Force -ErrorAction Stop
        $service.WaitForStatus("Stopped", "00:00:30")
    }
}

if ([string]::IsNullOrWhiteSpace($ServiceName)) {
    throw "服务名称不能为空。"
}
if ([string]::IsNullOrWhiteSpace($DisplayName)) {
    throw "服务显示名称不能为空。"
}
if ($LogMaxBytes -le 0) {
    throw "LogMaxBytes 必须大于 0。"
}
if ($LogMaxBackups -lt 0) {
    throw "LogMaxBackups 不能小于 0。"
}

if (-not $DryRun) {
    Assert-Admin
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($ExePath)) {
    $ExePath = Resolve-DefaultExePath $scriptDir
}
if ([string]::IsNullOrWhiteSpace($DataDir)) {
    $DataDir = Join-Path $env:ProgramData "AllNotify\data"
}

$ExePath = Resolve-FullPath $ExePath
$DataDir = Resolve-FullPath $DataDir

if ($Uninstall) {
    $service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    if ($service) {
        Stop-ServiceIfRunning $ServiceName
        Invoke-Sc @("delete", $ServiceName)
        Write-Host "已删除 Windows 服务: $ServiceName"
    } else {
        Write-Host "Windows 服务不存在: $ServiceName"
    }
    exit 0
}

if (-not (Test-Path $ExePath)) {
    throw "找不到 All Notify Windows 可执行文件: $ExePath"
}

if ($DryRun) {
    Write-Host "DRY RUN: New-Item -ItemType Directory -Force -Path $DataDir"
} else {
    New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
}

$binaryParts = @(
    (Quote-Arg $ExePath),
    (New-FlagArg "service-name" $ServiceName),
    (New-FlagArg "addr" $Addr),
    (New-FlagArg "data-dir" $DataDir),
    (New-FlagArg "send-timeout" $SendTimeout),
    (New-FlagArg "log-max-bytes" ([string]$LogMaxBytes)),
    (New-FlagArg "log-max-backups" ([string]$LogMaxBackups))
)
$binaryPathName = $binaryParts -join " "

$existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existing) {
    Stop-ServiceIfRunning $ServiceName
    Invoke-Sc @("config", $ServiceName, "binPath=", $binaryPathName, "start=", "auto", "DisplayName=", $DisplayName)
    Invoke-Sc @("description", $ServiceName, $Description)
    Write-Host "已更新 Windows 服务: $ServiceName"
} else {
    Invoke-Sc @("create", $ServiceName, "binPath=", $binaryPathName, "start=", "auto", "DisplayName=", $DisplayName)
    Invoke-Sc @("description", $ServiceName, $Description)
    Write-Host "已创建 Windows 服务: $ServiceName"
}

if ($Restart) {
    if ($DryRun) {
        Write-Host "DRY RUN: Start-Service -Name $ServiceName"
    } else {
        Start-Service -Name $ServiceName
        (Get-Service -Name $ServiceName).WaitForStatus("Running", "00:00:30")
        Write-Host "已启动 Windows 服务: $ServiceName"
    }
}

Write-Host "服务命令: $binaryPathName"
Write-Host "数据目录: $DataDir"
Write-Host "运行日志: $(Join-Path $DataDir 'logs\app.log')"
