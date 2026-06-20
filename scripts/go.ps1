$ErrorActionPreference = 'Stop'
$env:Path = 'C:\Users\jagan\go-sdk\go\bin;' + $env:Path
$env:GOTOOLCHAIN = 'local'
Set-Location 'C:\Users\jagan\Projects\project\throttle'
& 'C:\Users\jagan\go-sdk\go\bin\go.exe' @args
exit $LASTEXITCODE
