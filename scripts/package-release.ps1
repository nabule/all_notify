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

function Write-Utf8NoBomLines([string]$Path, [string[]]$Lines) {
    $encoding = New-Object System.Text.UTF8Encoding($false)
    [IO.File]::WriteAllLines($Path, $Lines, $encoding)
}

function Resolve-CommandPath([string]$CommandName, [string[]]$Fallbacks) {
    $command = Get-Command $CommandName -ErrorAction SilentlyContinue
    if ($command) {
        return $command.Source
    }

    foreach ($fallback in $Fallbacks) {
        if (Test-Path -LiteralPath $fallback -PathType Leaf) {
            return $fallback
        }
    }

    throw "找不到命令: $CommandName"
}

function Build-Binary([string]$Goos, [string]$Goarch, [string]$OutputPath) {
    $env:CGO_ENABLED = "0"
    $env:GOOS = $Goos
    $env:GOARCH = $Goarch
    $args = "build -trimpath -ldflags `"-s -w`" -o `"$OutputPath`" ./cmd/all-notify"
    $process = Start-Process -FilePath $script:GoCommand -ArgumentList $args -NoNewWindow -Wait -PassThru
    if ($process.ExitCode -ne 0) {
        throw "构建 $Goos/$Goarch 失败，退出码: $($process.ExitCode)"
    }
    if (-not (Test-Path -LiteralPath $OutputPath -PathType Leaf)) {
        throw "构建 $Goos/$Goarch 未生成文件: $OutputPath"
    }
    Test-BinaryFormat $OutputPath $Goos
}

function New-TarGzArchive([string]$SourceDir, [string]$ArchiveRoot, [string]$OutputPath) {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $tempTar = [IO.Path]::ChangeExtension($OutputPath, ".tar")
    if (Test-Path -LiteralPath $tempTar) {
        Remove-Item -LiteralPath $tempTar -Force
    }
    $tarStream = [IO.File]::Create($tempTar)
    try {
        $files = Get-ChildItem -LiteralPath $SourceDir -Recurse -File | Sort-Object FullName
        foreach ($file in $files) {
            $relative = Get-RelativePath $SourceDir $file.FullName
            $entryName = ($ArchiveRoot.TrimEnd("/", "\") + "/" + $relative).Replace("\", "/")
            Write-TarEntry $tarStream $file.FullName $entryName
        }
        Write-TarPadding $tarStream 1024
    } finally {
        $tarStream.Dispose()
    }

    $inputStream = [IO.File]::OpenRead($tempTar)
    $outputStream = [IO.File]::Create($OutputPath)
    try {
        $gzipStream = New-Object IO.Compression.GzipStream($outputStream, [IO.Compression.CompressionMode]::Compress)
        try {
            $inputStream.CopyTo($gzipStream)
        } finally {
            $gzipStream.Dispose()
        }
    } finally {
        $inputStream.Dispose()
        $outputStream.Dispose()
        Remove-Item -LiteralPath $tempTar -Force -ErrorAction SilentlyContinue
    }

    if (-not (Test-Path -LiteralPath $OutputPath -PathType Leaf)) {
        throw "tar.gz 归档生成失败: $OutputPath"
    }
}

function Write-TarEntry([IO.Stream]$Stream, [string]$FilePath, [string]$EntryName) {
    $info = Get-Item -LiteralPath $FilePath
    $header = New-Object byte[] 512
    Write-TarString $header 0 100 $EntryName
    Write-TarOctal $header 100 8 420
    Write-TarOctal $header 108 8 0
    Write-TarOctal $header 116 8 0
    Write-TarOctal $header 124 12 $info.Length
    $mtime = [int64](($info.LastWriteTimeUtc) - [DateTime]"1970-01-01T00:00:00Z").TotalSeconds
    Write-TarOctal $header 136 12 $mtime
    for ($i = 148; $i -lt 156; $i++) {
        $header[$i] = 32
    }
    $header[156] = [byte][char]"0"
    Write-TarString $header 257 6 "ustar"
    Write-TarString $header 263 2 "00"
    $checksum = 0
    foreach ($b in $header) {
        $checksum += $b
    }
    Write-TarOctal $header 148 8 $checksum
    $Stream.Write($header, 0, $header.Length)

    $input = [IO.File]::OpenRead($FilePath)
    try {
        $input.CopyTo($Stream)
    } finally {
        $input.Dispose()
    }
    $remainder = $info.Length % 512
    if ($remainder -ne 0) {
        Write-TarPadding $Stream (512 - $remainder)
    }
}

function Write-TarString([byte[]]$Header, [int]$Offset, [int]$Length, [string]$Value) {
    $bytes = [Text.Encoding]::ASCII.GetBytes($Value)
    $count = [Math]::Min($bytes.Length, $Length)
    [Array]::Copy($bytes, 0, $Header, $Offset, $count)
}

function Write-TarOctal([byte[]]$Header, [int]$Offset, [int]$Length, [int64]$Value) {
    $text = [Convert]::ToString($Value, 8).PadLeft($Length - 1, "0")
    $bytes = [Text.Encoding]::ASCII.GetBytes($text)
    [Array]::Copy($bytes, 0, $Header, $Offset, [Math]::Min($bytes.Length, $Length - 1))
    $Header[$Offset + $Length - 1] = 0
}

function Write-TarPadding([IO.Stream]$Stream, [int64]$Length) {
    if ($Length -le 0) {
        return
    }
    $zeros = New-Object byte[] 512
    while ($Length -gt 0) {
        $count = [int][Math]::Min($zeros.Length, $Length)
        $Stream.Write($zeros, 0, $count)
        $Length -= $count
    }
}

function Test-BinaryFormat([string]$Path, [string]$Goos) {
    $stream = [IO.File]::OpenRead($Path)
    try {
        $header = New-Object byte[] 4
        $read = $stream.Read($header, 0, $header.Length)
    } finally {
        $stream.Dispose()
    }
    if ($read -lt 4) {
        throw "构建产物过小: $Path"
    }
    $hex = ($header | ForEach-Object { $_.ToString("x2") }) -join ""
    switch ($Goos) {
        "linux" {
            if ($hex -ne "7f454c46") {
                throw "构建产物格式错误，期望 Linux ELF: $Path"
            }
        }
        "windows" {
            if ($hex.Substring(0, 4) -ne "4d5a") {
                throw "构建产物格式错误，期望 Windows PE: $Path"
            }
        }
        "darwin" {
            if (@("feedfacf", "cffaedfe") -notcontains $hex) {
                throw "构建产物格式错误，期望 macOS Mach-O: $Path"
            }
        }
    }
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
$script:GoCommand = Resolve-CommandPath "go" @("C:\Program Files\Go\bin\go.exe")

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
Write-Utf8NoBomLines (Join-Path $binDir "sha256sums.txt") $hashLines

$manifest = Get-ChildItem -LiteralPath $releaseDir -Recurse -File |
    Sort-Object FullName |
    ForEach-Object { Get-RelativePath $releaseDir $_.FullName }
Write-Utf8NoBomLines (Join-Path $releaseDir "MANIFEST.txt") $manifest

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
Write-Utf8NoBomLines (Join-Path $releaseDir "RELEASE.md") $releaseMd

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
New-TarGzArchive $releaseDir $releaseName $tarPath
if (-not (Test-Path -LiteralPath $zipPath -PathType Leaf)) {
    throw "zip 归档生成失败: $zipPath"
}

$rootHashLines = @()
foreach ($path in @($tarPath, $zipPath)) {
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
    $rootHashLines += "$hash  $(Split-Path -Leaf $path)"
}
$rootHashLines += Get-Content -LiteralPath (Join-Path $binDir "sha256sums.txt") | ForEach-Object { "$_".Replace("  ", "  $releaseName/bin/") }
Write-Utf8NoBomLines (Join-Path $archiveRoot "sha256sums.txt") $rootHashLines

Write-Host "发布目录: $releaseDir"
Write-Host "ZIP: $zipPath"
Write-Host "TAR.GZ: $tarPath"
