# Runs only in an isolated Windows CI worker, never against a user's console.
param([Parameter(Mandatory=$true)][string]$Binary)
$ErrorActionPreference='Stop'
if ($env:CI -ne 'true' -or $env:OS -ne 'Windows_NT') {throw 'This smoke test requires an isolated Windows CI worker.'}
$Binary=(Resolve-Path $Binary).Path
$root=Join-Path ([IO.Path]::GetTempPath()) ('aegishook-ci-'+[Guid]::NewGuid().ToString('N'))
$data=Join-Path $root 'server'
$client=Join-Path $root 'client'
$project=Join-Path $root 'project'
$previousClaude=$env:CLAUDE_CONFIG_DIR
$env:CLAUDE_CONFIG_DIR=Join-Path $root 'claude'
$service=$null
try {
    New-Item -ItemType Directory -Path $root,$project,$env:CLAUDE_CONFIG_DIR | Out-Null
    if (Get-NetTCPConnection -LocalPort 18790 -State Listen -ErrorAction SilentlyContinue) {throw 'Test port already occupied.'}
    $service=Start-Process -FilePath $Binary -ArgumentList @('serve','--addr','127.0.0.1:18790','--data-dir',('"'+$data+'"'),'--agent-dir',('"'+(Join-Path $root 'pi')+'"')) -PassThru -RedirectStandardOutput (Join-Path $root 'stdout') -RedirectStandardError (Join-Path $root 'stderr')
    $ready=$false
    for ($i=0;$i -lt 60;$i++) {
        Start-Sleep -Milliseconds 250
        try { $null=Invoke-WebRequest -UseBasicParsing 'http://127.0.0.1:18790/'; $ready=$true; break } catch {}
    }
    if (-not $ready) {throw 'Test console did not start.'}
    $token=(Get-Content -Raw (Join-Path $data 'admin.token')).Trim()
    $null=Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:18790/api/v1/login' -ContentType 'application/json' -Body (@{token=$token}|ConvertTo-Json) -SessionVariable session
    $code=Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:18790/api/v1/client-enrollments' -ContentType 'application/json' -Body '{"name":"Windows CI"}' -WebSession $session
    $codePath=Join-Path $root 'enrollment'
    [IO.File]::WriteAllText($codePath,$code.code)
    $script=Join-Path $PSScriptRoot 'hook.ps1'
    $settings=Join-Path $env:CLAUDE_CONFIG_DIR 'settings.json'
    [IO.File]::WriteAllText($settings,'{"foreign":"preserve"}')
    & $script -Action install -Agent claude -Scope global -Endpoint 'http://127.0.0.1:18790' -CodeFile $codePath -Binary $Binary -ClientDir $client
    & $script -Action install -Agent claude -Scope global -Endpoint 'http://127.0.0.1:18790' -ClientDir $client
    & $script -Action install -Agent claude -Scope project -Project $project -SwitchScope -Endpoint 'http://127.0.0.1:18790' -ClientDir $client
    $doc=Get-Content -Raw $settings | ConvertFrom-Json
    if ($doc.foreign -ne 'preserve' -or ($doc.PSObject.Properties.Name -contains 'hooks')) {throw 'Global scope switch changed foreign config or retained Hooks.'}
    & $script -Action uninstall -Agent claude -Scope project -Project $project -ClientDir $client
    & $script -Action uninstall -Agent claude -Scope project -Project $project -ClientDir $client
    foreach ($path in @((Join-Path $project '.claude/settings.json'),(Join-Path $client 'client.json'),(Join-Path $client 'adapter/connection.json'),(Join-Path $client 'aegishook.exe'))) {if (Test-Path $path) {throw 'Uninstall left an owned resource.'}}
    Write-Output 'PASS: Windows installer, repeated install, scope switch, foreign configuration preservation and repeated uninstall.'
} finally {
    if ($service -and -not $service.HasExited) {$service.Kill();$service.WaitForExit()}
    $env:CLAUDE_CONFIG_DIR=$previousClaude
    if (Test-Path $root) {Remove-Item -LiteralPath $root -Recurse -Force}
}
