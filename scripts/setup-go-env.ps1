$ErrorActionPreference = 'Stop'
$goBin = 'C:\Users\jagan\go-sdk\go\bin'
$goPathBin = Join-Path $env:USERPROFILE 'go\bin'

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($null -eq $userPath) { $userPath = '' }
$parts = $userPath -split ';' | Where-Object { $_ -ne '' }

foreach ($p in @($goBin, $goPathBin)) {
    if ($parts -notcontains $p) { $parts += $p }
}
$newPath = ($parts -join ';')
[Environment]::SetEnvironmentVariable('Path', $newPath, 'User')

# Make go prefer the local toolchain (network is sandboxed; avoid auto-toolchain fetch)
[Environment]::SetEnvironmentVariable('GOTOOLCHAIN', 'local', 'User')

# Verify using the absolute path
$env:Path = $goBin + ';' + $env:Path
& "$goBin\go.exe" version
& "$goBin\go.exe" env GOOS GOARCH GOVERSION GOPATH
Write-Output ('User PATH updated. go bin = ' + $goBin)
