$log = "F:\feizhu\test-output.log"
$client = "F:\feizhu\cmd\feizhu-client\dist\feizhu-client-windows-amd64.exe"
Remove-Item $log -ErrorAction SilentlyContinue
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $client
$psi.Arguments = "-server 8.210.61.56:8443 -password xiaomingGG123456 -tun -auto-proxy=false -auto-env=false -auto-curlrc=false"
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$p = [System.Diagnostics.Process]::Start($psi)
Start-Sleep -Seconds 20
if (-not $p.HasExited) {
    $stdout = $p.StandardOutput.ReadToEnd()
    $stderr = $p.StandardError.ReadToEnd()
    $p.Kill()
} else {
    $stdout = $p.StandardOutput.ReadToEnd()
    $stderr = $p.StandardError.ReadToEnd()
}
@(
    "=== stdout ===",
    $stdout,
    "=== stderr ===",
    $stderr,
    "=== exit=$($p.ExitCode) ==="
) | Out-File $log -Encoding utf8
