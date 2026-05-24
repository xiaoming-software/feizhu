#Requires -Version 5.1
<#
.SYNOPSIS
  Build feizhu-client-ui on Windows (amd64 GUI exe).

.DESCRIPTION
  Windows-native build script (equivalent to build.sh build_windows_native):
  - Prepare WinDivert embed files for TUN go:embed
  - Generate rsrc_windows_amd64.syso (optional)
  - CGO + Fyne build with -H windowsgui

  Requires: Go 1.22+, MinGW-w64 gcc/g++ (manual install, must be in PATH or common locations)

  Usage:
    powershell -ExecutionPolicy Bypass -File .\build.ps1
    .\build.ps1 -Goproxy https://goproxy.cn,direct
    .\build.ps1 -SkipIcon
    .\build.ps1 -MingwBin "C:\msys64\mingw64\bin"

  Output:
    dist\feizhu-client-ui-windows-amd64.exe
#>
[CmdletBinding()]
param(
    [switch]$SkipIcon,
    [string]$Goproxy = "",
    [string]$OutFile = "",
    [string]$MingwBin = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$Root = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$Dist = Join-Path $ScriptDir "dist"
$Assets = Join-Path $ScriptDir "assets"
$EmbedDir = Join-Path $Root "internal\platform\windows\tunmode\embed"
$VendorDir = Join-Path $ScriptDir "vendor\windivert"
$DefaultOut = Join-Path $Dist "feizhu-client-ui-windows-amd64.exe"

if ([string]::IsNullOrWhiteSpace($OutFile)) {
    $OutFile = $DefaultOut
}

# Optional local overrides (gitignored). Only applies when -MingwBin was not passed.
$localCfg = Join-Path $ScriptDir "build.local.ps1"
if ((Test-Path $localCfg) -and [string]::IsNullOrWhiteSpace($MingwBin)) {
    . $localCfg
}

function Write-Step([string]$Message) {
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Write-Ok([string]$Message) {
    Write-Host "    $Message" -ForegroundColor Green
}

function Write-WarnMsg([string]$Message) {
    Write-Host "WARN: $Message" -ForegroundColor Yellow
}

function Fail([string]$Message) {
    Write-Host "ERROR: $Message" -ForegroundColor Red
    exit 1
}

function Find-GoExe {
    if ($env:GO -and (Test-Path $env:GO)) { return $env:GO }
    $cmd = Get-Command go -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    foreach ($p in @(
            "C:\Go\bin\go.exe",
            "$env:LOCALAPPDATA\Programs\Go\bin\go.exe",
            "$env:ProgramFiles\Go\bin\go.exe"
        )) {
        if (Test-Path $p) { return $p }
    }
    return $null
}

function Find-GccExe {
    if ($env:CC -and (Test-Path $env:CC)) { return $env:CC }
    if (-not [string]::IsNullOrWhiteSpace($MingwBin)) {
        $p = Join-Path $MingwBin.TrimEnd('\') "gcc.exe"
        if (Test-Path $p) { return $p }
    }
    if ($env:MINGW_BIN) {
        $p = Join-Path $env:MINGW_BIN.TrimEnd('\') "gcc.exe"
        if (Test-Path $p) { return $p }
    }
    $cmd = Get-Command gcc -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    foreach ($p in @(
            (Join-Path $ScriptDir "tools\mingw64\bin\gcc.exe"),
            "D:\app\mingw64\bin\gcc.exe",
            "$env:ProgramFiles\mingw64\bin\gcc.exe",
            "${env:ProgramFiles(x86)}\mingw-w64\mingw64\bin\gcc.exe",
            "$env:ProgramFiles\LLVM\mingw\bin\gcc.exe",
            "C:\msys64\mingw64\bin\gcc.exe",
            "C:\msys64\ucrt64\bin\gcc.exe",
            "C:\TDM-GCC-64\bin\gcc.exe",
            "C:\mingw64\bin\gcc.exe"
        )) {
        if (Test-Path $p) { return $p }
    }
    return $null
}

function Ensure-MingwToolchain {
    if (Find-GccExe) { return }
    $msg = @(
        "gcc not found. Fyne requires MinGW-w64 (gcc/g++)."
        "Please install MinGW manually and either:"
        "  1. Add its bin directory to PATH, then reopen terminal"
        "  2. Run: .\build.ps1 -MingwBin `"C:\path\to\mingw\bin`""
        "  3. Set env MINGW_BIN or CC to gcc.exe"
        "Common locations: C:\msys64\mingw64\bin, C:\Program Files\mingw64\bin"
    ) -join [Environment]::NewLine
    Fail $msg
}

function Ensure-WinDivertEmbed {
    $embedDll = Join-Path $EmbedDir "WinDivert.dll"
    $embedSys = Join-Path $EmbedDir "WinDivert64.sys"
    $cacheDll = Join-Path $VendorDir "WinDivert.dll"
    $cacheSys = Join-Path $VendorDir "WinDivert64.sys"

    if ((Test-Path $embedDll) -and (Test-Path $embedSys)) {
        Write-Ok "Using existing embed\WinDivert.dll + WinDivert64.sys"
        return
    }

    if (-not ((Test-Path $cacheDll) -and (Test-Path $cacheSys))) {
        Write-Step "Downloading WinDivert 2.2.2..."
        $tmp = Join-Path $env:TEMP ("feizhu-windivert-" + [guid]::NewGuid().ToString("n"))
        New-Item -ItemType Directory -Path $tmp -Force | Out-Null
        $zip = Join-Path $tmp "WinDivert.zip"
        $url = "https://reqrypt.org/download/WinDivert-2.2.2-A.zip"
        try {
            Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing
            Expand-Archive -Path $zip -DestinationPath $tmp -Force
        }
        catch {
            Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
            $hint = "Place x64 WinDivert.dll and WinDivert64.sys under: $VendorDir"
            Fail "WinDivert download/extract failed: $_`n$hint"
        }
        $dllSrc = Join-Path $tmp "WinDivert-2.2.2-A\x64\WinDivert.dll"
        $sysSrc = Join-Path $tmp "WinDivert-2.2.2-A\x64\WinDivert64.sys"
        if (-not ((Test-Path $dllSrc) -and (Test-Path $sysSrc))) {
            Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
            Fail "WinDivert zip missing x64\WinDivert.dll or WinDivert64.sys"
        }
        New-Item -ItemType Directory -Path $VendorDir -Force | Out-Null
        Copy-Item $dllSrc $cacheDll -Force
        Copy-Item $sysSrc $cacheSys -Force
        Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }

    New-Item -ItemType Directory -Path $EmbedDir -Force | Out-Null
    Copy-Item $cacheDll $embedDll -Force
    Copy-Item $cacheSys $embedSys -Force
    Write-Ok "Copied WinDivert into internal\platform\windows\tunmode\embed (go:embed)"
}

function Ensure-WintunEmbed {
    $embedDll = Join-Path $EmbedDir "wintun.dll"
    $cacheDll = Join-Path $VendorDir "wintun.dll"

    if (Test-Path $embedDll) {
        Write-Ok "Using existing embed\wintun.dll"
        return
    }

    if (-not (Test-Path $cacheDll)) {
        Write-Step "Downloading wintun 0.14.1 (amd64)..."
        $tmp = Join-Path $env:TEMP ("feizhu-wintun-" + [guid]::NewGuid().ToString("n"))
        New-Item -ItemType Directory -Path $tmp -Force | Out-Null
        $zip = Join-Path $tmp "wintun.zip"
        $url = "https://www.wintun.net/builds/wintun-0.14.1.zip"
        try {
            Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing
            Expand-Archive -Path $zip -DestinationPath $tmp -Force
        }
        catch {
            Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
            $hint = "Place amd64 wintun.dll under: $VendorDir"
            Fail "wintun download/extract failed: $_`n$hint"
        }
        $dllSrc = Join-Path $tmp "wintun\bin\amd64\wintun.dll"
        if (-not (Test-Path $dllSrc)) {
            Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
            Fail "wintun zip missing wintun\bin\amd64\wintun.dll"
        }
        New-Item -ItemType Directory -Path $VendorDir -Force | Out-Null
        Copy-Item $dllSrc $cacheDll -Force
        Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }

    New-Item -ItemType Directory -Path $EmbedDir -Force | Out-Null
    Copy-Item $cacheDll $embedDll -Force
    Write-Ok "Copied wintun.dll into internal\platform\windows\tunmode\embed (go:embed)"
}

function Ensure-WindowsIcon {
    if ($SkipIcon) {
        Write-WarnMsg "Skipped icon resource generation (-SkipIcon)"
        return
    }

    $iconPng = Join-Path $Assets "icon.png"
    $iconSrc = Join-Path $Assets "icon-src.png"
    $syso = Join-Path $ScriptDir "rsrc_windows_amd64.syso"

    if (-not (Test-Path $iconPng)) {
        if (-not (Test-Path $iconSrc)) {
            Fail "Missing $iconSrc or $iconPng for Windows icon resource"
        }
        Write-Step "Generating icon.png from icon-src.png..."
        Push-Location $Root
        try {
            & $script:GoExe run ./cmd/feizhu-icongen/ -src "cmd/feizhu-client-ui/assets/icon-src.png" -out "cmd/feizhu-client-ui/assets/icon.png"
            if ($LASTEXITCODE -ne 0) { Fail "feizhu-icongen failed" }
        }
        finally {
            Pop-Location
        }
    }

    $needSyso = -not (Test-Path $syso)
    if (-not $needSyso -and (Test-Path $iconPng)) {
        $needSyso = (Get-Item $iconPng).LastWriteTime -gt (Get-Item $syso).LastWriteTime
    }
    if (-not $needSyso) {
        Write-Ok "Using existing rsrc_windows_amd64.syso"
        return
    }

    Write-Step "Generating rsrc_windows_amd64.syso..."
    Push-Location $ScriptDir
    try {
        & $script:GoExe run github.com/tc-hib/go-winres@v0.3.2 simply --icon assets/icon.png --arch amd64
        if ($LASTEXITCODE -ne 0) {
            Write-WarnMsg "go-winres failed; continuing without custom exe icon"
        }
        else {
            Write-Ok "Generated rsrc_windows_amd64.syso"
        }
    }
    finally {
        Pop-Location
    }
}

function Clean-ObsoleteDist {
    foreach ($name in @("WinDivert.dll", "WinDivert64.sys", "wintun.dll")) {
        $p = Join-Path $Dist $name
        if (-not (Test-Path $p)) {
            continue
        }
        try {
            Remove-Item $p -Force -ErrorAction Stop
            Write-Ok "Removed obsolete dist file: $name"
        }
        catch {
            Write-WarnMsg "Could not remove dist\$name (file in use? stop feizhu/TUN first): $_"
        }
    }
}

# --- main ---

if ($env:OS -ne "Windows_NT") {
    Fail "This script is for Windows native builds only"
}

$script:GoExe = Find-GoExe
if (-not $script:GoExe) {
    Fail "Go not found. Install Go 1.22+ and add to PATH, or set env GO to go.exe. See https://go.dev/dl/"
}

Ensure-MingwToolchain

$GccExe = Find-GccExe

$GppExe = Join-Path (Split-Path $GccExe) "g++.exe"
if (-not (Test-Path $GppExe)) {
    Fail "g++ not found: $GppExe"
}

Write-Step "Environment"
Write-Ok "Go  = $GoExe"
Write-Ok "gcc = $GccExe"
Write-Ok "g++ = $GppExe"

if (-not [string]::IsNullOrWhiteSpace($Goproxy)) {
    $env:GOPROXY = $Goproxy
    Write-Ok "GOPROXY = $Goproxy"
}
elseif (-not $env:GOPROXY) {
    $env:GOPROXY = "https://goproxy.cn,direct"
    Write-Ok "GOPROXY = $($env:GOPROXY) (default; override with -Goproxy)"
}

New-Item -ItemType Directory -Path $Dist -Force | Out-Null
Clean-ObsoleteDist
Ensure-WinDivertEmbed
Ensure-WintunEmbed
Ensure-WindowsIcon

Write-Step "Building Windows amd64 (native CGO)..."
Push-Location $Root
try {
    $env:CGO_ENABLED = "1"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $env:CC = $GccExe
    $env:CXX = $GppExe

    & $GoExe build -trimpath -ldflags="-s -w -H windowsgui" -o $OutFile ./cmd/feizhu-client-ui
    if ($LASTEXITCODE -ne 0) {
        Fail "go build failed (exit $LASTEXITCODE)"
    }
}
finally {
    Pop-Location
}

$fi = Get-Item $OutFile
Write-Host ""
Write-Ok "Output: $($fi.FullName)"
Write-Ok "Size:   $([math]::Round($fi.Length / 1MB, 2)) MB"
Write-Host ""
Write-Host "Done. Run the exe directly; TUN mode requires UAC admin approval." -ForegroundColor Green
