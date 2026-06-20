$ErrorActionPreference = 'Stop'
$env:Path = 'C:\Users\jagan\go-sdk\go\bin;' + $env:Path
$env:GOTOOLCHAIN = 'local'
$root = 'C:\Users\jagan\Projects\project\throttle'
Set-Location $root

# tidy module (best-effort; needs no new downloads)
& 'C:\Users\jagan\go-sdk\go\bin\go.exe' mod tidy

git add -A
git status --short
$msg = $args[0]
git commit -m $msg | Out-Null
git log --oneline -1
