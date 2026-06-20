$ErrorActionPreference = 'Stop'
$a = 'C:\Users\jagan\.codex\auth.json'
if (Test-Path $a) {
  $o = Get-Content $a -Raw | ConvertFrom-Json
  Write-Output ("auth.json top-level keys: " + ($o.PSObject.Properties.Name -join ', '))
  # Print ONLY non-secret indicators
  Write-Output ("OPENAI_API_KEY present: " + [bool]$o.OPENAI_API_KEY)
  if ($o.tokens) {
    Write-Output ("tokens keys: " + ($o.tokens.PSObject.Properties.Name -join ', '))
    Write-Output ("has refresh_token: " + [bool]$o.tokens.refresh_token)
    Write-Output ("has access_token: " + [bool]$o.tokens.access_token)
    Write-Output ("account_id present: " + [bool]$o.tokens.account_id)
  }
  Write-Output ("auth_mode (top): " + $o.auth_mode)
  Write-Output ("last_refresh: " + $o.last_refresh)
} else { Write-Output 'no auth.json' }
