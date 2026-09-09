$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')
npm --prefix web ci --no-audit --no-fund
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
npm --prefix web run build
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
node scripts/prepare-assets.mjs
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
New-Item bin -ItemType Directory -Force | Out-Null
go build -trimpath -o bin/aegishook.exe ./cmd/aegishook
exit $LASTEXITCODE
