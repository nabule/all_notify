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
    [switch]$DryRun
)

$script = Join-Path (Split-Path -Parent $MyInvocation.MyCommand.Path) "install-windows-service.ps1"
$params = @{
    ServiceName   = $ServiceName
    DisplayName   = $DisplayName
    Description   = $Description
    Addr          = $Addr
    SendTimeout   = $SendTimeout
    LogMaxBytes   = $LogMaxBytes
    LogMaxBackups = $LogMaxBackups
}
if (-not [string]::IsNullOrWhiteSpace($ExePath)) {
    $params.ExePath = $ExePath
}
if (-not [string]::IsNullOrWhiteSpace($DataDir)) {
    $params.DataDir = $DataDir
}
if ($Restart) {
    $params.Restart = $true
}
if ($DryRun) {
    $params.DryRun = $true
}
& $script @params
