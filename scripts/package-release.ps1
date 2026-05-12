#Requires -Version 5.1

[CmdletBinding()]
param(
    [string]$Version = "dev",
    [string]$OutputRoot = "release",
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"

function Resolve-FullPath([string]$Path) {
    if ([IO.Path]::IsPathRooted($Path)) {
        return [IO.Path]::GetFullPath($Path)
    }
    return [IO.Path]::GetFullPath((Join-Path (Get-Location) $Path))
}

function Copy-RequiredFile([string]$Source, [string]$Destination) {
    if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
        throw "缺少文件: $Source"
    }
    $parent = Split-Path -Parent $Destination
    New-Item -ItemType Directory -Force -Path $parent | Out-Null
    Copy-Item -LiteralPath $Source -Destination $Destination -Force
}

function Copy-RequiredDirectory([string]$Source, [string]$Destination) {
    if (-not (Test-Path -LiteralPath $Source -PathType Container)) {
        throw "缺少目录: $Source"
    }
    if (Test-Path -LiteralPath $Destination) {
        Remove-Item -LiteralPath $Destination -Recurse -Force
    }
    $parent = Split-Path -Parent $Destination
    New-Item -ItemType Directory -Force -Path $parent | Out-Null
    Copy-Item -LiteralPath $Source -Destination $Destination -Recurse -Force
}

function Build-Binary([string]$Goos, [string]$Goarch, [string]$OutputPath) {
    $env:CGO_ENABLED = "0"
    $env:GOOS = $Goos
    $env:GOARCH = $Goarch
    go build -trimpath -ldflags="-s -w" -o $OutputPath ./cmd/all-notify
}

function Get-BinaryName([string]$Goos, [string]$Goarch) {
    $extension = ""
    if ($Goos -eq "windows") {
        $extension = ".exe"
    }
    return "all-notify-$Goos-$Goarch$extension"
}

function Get-RelativePath([string]$BasePath, [string]$FullPath) {
    $baseUri = [Uri]($BasePath.TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar)
    $fullUri = [Uri]$FullPath
    return [Uri]::UnescapeDataString($baseUri.MakeRelativeUri($fullUri).ToString()).Replace('\', '/')
}

$repoRoot = Resolve-FullPath "."
$outputRootFull = Resolve-FullPath $OutputRoot
$releaseName = "all-notify-$Version"
$releaseDir = Join-Path $outputRootFull $Version | Join-Path -ChildPath $releaseName
$binDir = Join-Path $releaseDir "bin"
$distDir = Join-Path $repoRoot "dist"

if (-not $SkipBuild) {
    New-Item -ItemType Directory -Force -Path $distDir | Out-Null
    $targets = @(
        @("linux", "amd64"),
        @("linux", "arm64"),
        @("windows", "amd64"),
        @("windows", "arm64"),
        @("darwin", "amd64"),
        @("darwin", "arm64")
    )
    foreach ($target in $targets) {
        $name = Get-BinaryName $target[0] $target[1]
        Build-Binary $target[0] $target[1] (Join-Path $distDir $name)
    }
}

if (Test-Path -LiteralPath $releaseDir) {
    Remove-Item -LiteralPath $releaseDir -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

Copy-RequiredFile (Join-Path $repoRoot "README.md") (Join-Path $releaseDir "README.md")
Copy-RequiredFile (Join-Path $repoRoot "Dockerfile") (Join-Path $releaseDir "Dockerfile")
Copy-RequiredFile (Join-Path $repoRoot "docker-compose.yml") (Join-Path $releaseDir "docker-compose.yml")
Copy-RequiredDirectory (Join-Path $repoRoot "docs") (Join-Path $releaseDir "docs")
Copy-RequiredDirectory (Join-Path $repoRoot "scripts") (Join-Path $releaseDir "scripts")
Copy-RequiredDirectory (Join-Path $repoRoot "skill") (Join-Path $releaseDir "skill")

$binaryNames = @(
    "all-notify-linux-amd64",
    "all-notify-linux-arm64",
    "all-notify-windows-amd64.exe",
    "all-notify-windows-arm64.exe",
    "all-notify-darwin-amd64",
    "all-notify-darwin-arm64"
)
foreach ($binaryName in $binaryNames) {
    Copy-RequiredFile (Join-Path $distDir $binaryName) (Join-Path $binDir $binaryName)
}

Copy-RequiredFile (Join-Path $repoRoot "docs\usage.md") (Join-Path $releaseDir "skill\all-notify-usage\references\usage.md")

$hashLines = Get-ChildItem -LiteralPath $binDir -File | Sort-Object Name | ForEach-Object {
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant()
    "$hash  $($_.Name)"
}
$hashLines | Set-Content -Path (Join-Path $binDir "sha256sums.txt") -Encoding utf8

$manifest = Get-ChildItem -LiteralPath $releaseDir -Recurse -File |
    Sort-Object FullName |
    ForEach-Object { Get-RelativePath $releaseDir $_.FullName }
$manifest | Set-Content -Path (Join-Path $releaseDir "MANIFEST.txt") -Encoding utf8

$releaseMd = @(
    "# All Notify $Version Release"
    ""
    "版本：``$Version``"
    ""
    "## 包内容"
    ""
    "- ``bin/``：单文件执行程序。"
    "- ``bin/sha256sums.txt``：执行文件 SHA256 校验值。"
    "- ``docs/``：架构、设计、测试和完整使用说明。"
    "- ``scripts/``：Windows 服务安装和发布打包脚本。"
    "- ``skill/all-notify-usage/``：Codex skill，可用于 All Notify 使用、配置、部署和排障指导。"
    "- ``README.md``：快速启动和 API 摘要。"
    "- ``docker-compose.yml``、``Dockerfile``：容器部署示例。"
    "- ``MANIFEST.txt``：release 文件清单。"
    ""
    "## Windows 服务"
    ""
    '```powershell'
    '$script = (Resolve-Path .\scripts\install-windows-service.ps1).Path'
    'Start-Process powershell -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$script`" -ExePath .\bin\all-notify-windows-amd64.exe -Restart"'
    '```'
    ""
    "## Skill"
    ""
    "Codex skill 位于 ``skill/all-notify-usage``，可复制到 Codex skills 目录后使用。"
)
$releaseMd | Set-Content -Path (Join-Path $releaseDir "RELEASE.md") -Encoding utf8

$archiveRoot = Split-Path -Parent $releaseDir
$zipPath = Join-Path $archiveRoot "$releaseName.zip"
$tarPath = Join-Path $archiveRoot "$releaseName.tar.gz"
if (Test-Path -LiteralPath $zipPath) {
    Remove-Item -LiteralPath $zipPath -Force
}
if (Test-Path -LiteralPath $tarPath) {
    Remove-Item -LiteralPath $tarPath -Force
}
Compress-Archive -Path $releaseDir -DestinationPath $zipPath -Force
tar -czf $tarPath -C $archiveRoot $releaseName

$rootHashLines = @()
foreach ($path in @($tarPath, $zipPath)) {
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
    $rootHashLines += "$hash  $(Split-Path -Leaf $path)"
}
$rootHashLines += Get-Content -LiteralPath (Join-Path $binDir "sha256sums.txt") | ForEach-Object { "$_".Replace("  ", "  $releaseName/bin/") }
$rootHashLines | Set-Content -Path (Join-Path $archiveRoot "sha256sums.txt") -Encoding utf8

Write-Host "发布目录: $releaseDir"
Write-Host "ZIP: $zipPath"
Write-Host "TAR.GZ: $tarPath"
