$ErrorActionPreference = 'Stop'
$go = 'C:\Users\jagan\go-sdk\go\bin\go.exe'
$env:Path = 'C:\Users\jagan\go-sdk\go\bin;' + $env:Path
$env:GOTOOLCHAIN = 'local'
$env:CGO_ENABLED = '0'   # pure-Go: cross-compile with no toolchain
$root = 'C:\Users\jagan\Projects\project\throttle'
Set-Location $root

# Node-convention names (matches installer defaultBinSrc: <platform>-<arch>)
$targets = @(
  @{ goos='windows'; goarch='amd64'; node='win32-x64' },
  @{ goos='windows'; goarch='arm64'; node='win32-arm64' },
  @{ goos='darwin';  goarch='amd64'; node='darwin-x64' },
  @{ goos='darwin';  goarch='arm64'; node='darwin-arm64' },
  @{ goos='linux';   goarch='amd64'; node='linux-x64' },
  @{ goos='linux';   goarch='arm64'; node='linux-arm64' }
)

foreach ($t in $targets) {
  $env:GOOS = $t.goos
  $env:GOARCH = $t.goarch
  $suffix = if ($t.goos -eq 'windows') { '.exe' } else { '' }
  $out = Join-Path $root ("installer\dist\" + $t.node)
  New-Item -ItemType Directory -Force -Path $out | Out-Null
  & $go build -o (Join-Path $out ("throttled$suffix")) ./cmd/throttled
  & $go build -o (Join-Path $out ("throttle-hook$suffix")) ./cmd/throttle-hook
  Write-Output ("built " + $t.node)
}
Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
Write-Output 'matrix build complete:'
Get-ChildItem (Join-Path $root 'installer\dist') -Recurse -File | ForEach-Object { $_.FullName.Substring($root.Length+1) + '  ' + [math]::Round($_.Length/1MB,1) + 'MB' }
