#Requires -Version 5.1

[CmdletBinding()]
param(
    [string]$ServiceName = "AllNotify",
    [switch]$DryRun
)

$script = Join-Path (Split-Path -Parent $MyInvocation.MyCommand.Path) "install-windows-service.ps1"
$params = @{
    ServiceName = $ServiceName
    Uninstall   = $true
}
if ($DryRun) {
    $params.DryRun = $true
}
& $script @params
