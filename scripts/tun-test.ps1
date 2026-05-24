$ErrorActionPreference = "Continue"
$log = Join-Path $env:TEMP "feizhu-tun-test.log"
$done = Join-Path $env:TEMP "feizhu-tun-test.done"
$client = "F:\feizhu\cmd\feizhu-client\dist\feizhu-client-windows-amd64.exe"

Remove-Item $log, $done -ErrorAction SilentlyContinue
function Log($msg) { Add-Content -Path $log -Value $msg -Encoding utf8 }

Log "=== feizhu TUN test start $(Get-Date) ==="
Log "IsAdmin=$(([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator))"

if (-not (Test-Path $client)) {
    Log "ERROR: client not found: $client"
    "FAIL" | Out-File $done
    exit 1
}

$argList = "-server 8.210.61.56:8443 -password xiaomingGG123456 -tun -auto-proxy=false -auto-env=false -auto-curlrc=false"
Log "Starting: $client $argList"

$p = Start-Process -FilePath $client -ArgumentList $argList -PassThru -WindowStyle Hidden
if ($null -eq $p) {
    Log "ERROR: Start-Process returned null"
    "FAIL" | Out-File $done
    exit 1
}
Log "PID=$($p.Id)"

$ready = $false
$tunMode = "unknown"
for ($i = 0; $i -lt 45; $i++) {
    Start-Sleep -Seconds 1
    if ($p.HasExited) {
        Log "client exited early code=$($p.ExitCode)"
        break
    }
    # feizhu-client 日志在 stderr，此处用 netstat 检测监听
    $listen7890 = netstat -ano | Select-String ":7890.*LISTENING"
    $listen7891 = netstat -ano | Select-String ":7891.*LISTENING"
    if ($listen7890 -and $listen7891) {
        $adapters = Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object { $_.Name -like "*Feizhu*" -or $_.InterfaceDescription -like "*Wintun*" }
        if ($adapters) {
            $tunMode = "wintun"
            $ready = $true
            Log "Detected wintun adapter: $($adapters.Name -join ', ')"
            break
        }
        # 无 wintun 但端口在监听，可能 WinDivert 降级
        if ($i -ge 8) {
            $tunMode = "windivert-or-delayed"
            $ready = $true
            Log "Ports listening, no Feizhu adapter yet (may be WinDivert)"
            break
        }
    }
}

if (-not $ready) {
    Log "TUN/proxy not ready after 45s"
    Log "netstat7890=$(netstat -ano | Select-String ':7890')"
    Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
    "FAIL" | Out-File $done
    exit 1
}

Log "Mode=$tunMode, testing transparent curl..."
$env:http_proxy = $null; $env:https_proxy = $null
$env:HTTP_PROXY = $null; $env:HTTPS_PROXY = $null; $env:ALL_PROXY = $null

$ip = curl.exe -s --max-time 25 --noproxy "*" https://api.ipify.org 2>&1
Log "curl transparent ipify: $ip"

$baidu = curl.exe -s --max-time 25 --noproxy "*" -o NUL -w "%{http_code}" https://www.baidu.com 2>&1
Log "curl transparent baidu http_code: $baidu"

$routes = route print 0.0.0.0 | Select-String "0.0.0.0|128.0.0.0|Feizhu|10.255"
Log "routes:`n$($routes | Out-String)"

Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
Log "=== test end $(Get-Date) ==="
if ($ip -match "^\d+\.\d+\.\d+\.\d+$" -and $baidu -eq "200") {
    "OK" | Out-File $done
} else {
    "PARTIAL" | Out-File $done
}
