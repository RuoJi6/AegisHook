# Windows PowerShell 5.1+ / PowerShell 7. Client hooks only; no background service.
[CmdletBinding()]
param(
    [ValidateSet('install','uninstall')][string]$Action,
    [ValidateSet('pi','claude','codex','opencode','grok')][string]$Agent = 'claude',
    [ValidateSet('global','project')][string]$Scope = 'global',
    [string]$Project,
    [string]$Endpoint,
    [string]$DownloadBase = $env:AEGIS_INSTALL_DOWNLOAD_BASE,
    [string]$CodeFile,
    [string]$ClientDir = (Join-Path $env:USERPROFILE '.aegishook-client'),
    [string]$Binary,
    [string]$Version = 'latest',
    [switch]$SwitchScope,
    [switch]$Help
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ($Help) {
    Write-Output @'
hook.ps1 -Action install|uninstall [-Agent claude|pi|codex|opencode|grok]
  -Scope global|project -Project C:\absolute\project
  -Endpoint https://review.example [-CodeFile C:\private\enrollment-code]
  -ClientDir C:\private\client -Binary C:\path\aegishook.exe -Version vX.Y.Z
  -SwitchScope: remove the other scope after installing this one successfully.
First installation waits for approval in the console. CodeFile is optional.
Without -Action, display the install/uninstall menu. No Agent or server is installed.
'@
    return
}
if ($env:OS -ne 'Windows_NT') { throw 'Use hook.sh on macOS and Linux.' }
if (-not $Action) {
    Write-Output "1. Install Hook`n2. Uninstall Hook"
    switch (Read-Host 'Choose 1 or 2') { '1' {$Action='install'} '2' {$Action='uninstall'} default {throw 'Choose 1 or 2.'} }
    $choice = Read-Host 'Agent [claude/pi/codex/opencode/grok, default claude]'
    if ($choice) { $Agent=$choice }
    $choice = Read-Host 'Scope [global/project, default global]'
    if ($choice) { $Scope=$choice }
}
if ($Scope -eq 'project' -and -not $Project) { $Project=Read-Host 'Absolute project path' }
if ($Scope -eq 'global' -and $Project) { throw 'Global scope cannot specify a project.' }
if ($Action -eq 'uninstall' -and ($CodeFile -or $SwitchScope)) { throw 'Uninstall does not require enrollment code or scope switching.' }
if ($Action -eq 'install' -and -not $Endpoint) { $Endpoint=Read-Host 'Review service URL' }
foreach ($value in @($ClientDir,$Project)) {
    if ($value -and ($value -notmatch '^(?:[A-Za-z]:[\\/]|\\\\[^\\]+\\[^\\]+)' -or $value -match '[\x00-\x1f]')) { throw 'Use absolute paths without control characters.' }
}
$ClientDir=[IO.Path]::GetFullPath($ClientDir).TrimEnd('\','/')
if ($ClientDir -eq [IO.Path]::GetPathRoot($ClientDir).TrimEnd('\','/')) { throw 'Client directory cannot be a filesystem root.' }
$executable=Join-Path $ClientDir 'aegishook.exe'
$marker=Join-Path $ClientDir 'binary.sha256'
$manifest=Join-Path $ClientDir 'client.json'
$temporary=$null

function Assert-RegularFile([string]$Path) {
    $item=Get-Item -LiteralPath $Path -Force
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Not a regular file: $Path" }
}
function Protect-ClientDirectory([string]$Path) {
    # Go's Unix 0600 flags are not a Windows ACL. Apply inheritance for client secrets.
    $sid=[Security.Principal.WindowsIdentity]::GetCurrent().User
    $acl=New-Object Security.AccessControl.DirectorySecurity
    $acl.SetOwner($sid)
    $acl.SetAccessRuleProtection($true,$false)
    $rule=New-Object Security.AccessControl.FileSystemAccessRule($sid,'FullControl','ContainerInherit,ObjectInherit','None','Allow')
    $acl.AddAccessRule($rule)
    Set-Acl -LiteralPath $Path -AclObject $acl
}
try {
    if (-not (Test-Path -LiteralPath $executable)) {
        if ($Action -eq 'uninstall' -and -not (Test-Path -LiteralPath $manifest)) { Write-Output 'No registered Hook installation.'; return }
        if (-not $Binary -and $PSScriptRoot) {
            $bundled=Join-Path (Split-Path $PSScriptRoot -Parent) 'aegishook.exe'
            if (Test-Path -LiteralPath $bundled -PathType Leaf) { $Binary=$bundled }
        }
        $temporary=Join-Path ([IO.Path]::GetTempPath()) ('aegishook-client-'+[Guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $temporary | Out-Null
        Protect-ClientDirectory $temporary
        if (-not $Binary) {
            if ($Action -ne 'install') { throw 'Runner missing. Use -Binary with the original executable to uninstall.' }
            $architecture=$env:PROCESSOR_ARCHITECTURE
            if ($env:PROCESSOR_ARCHITEW6432) { $architecture=$env:PROCESSOR_ARCHITEW6432 }
            if ($architecture -ne 'AMD64') { throw 'Releases currently support Windows AMD64 only.' }
            [Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12
            if ($DownloadBase) {
                $url=[Uri]$DownloadBase
                if ($url.UserInfo -or ($url.Scheme -ne 'https' -and -not ($url.Scheme -eq 'http' -and $url.Host -in @('127.0.0.1','localhost','[::1]')))) {throw 'Downloads require HTTPS or loopback HTTP.'}
                $Binary=Join-Path $temporary 'aegishook.exe'
                $checksum=Join-Path $temporary 'checksum'
                Invoke-WebRequest -UseBasicParsing -MaximumRedirection 0 -Uri "$DownloadBase/bin/windows/amd64" -OutFile $Binary
                Invoke-WebRequest -UseBasicParsing -MaximumRedirection 0 -Uri "$DownloadBase/sha256/windows/amd64" -OutFile $checksum
                $expected=([IO.File]::ReadAllText($checksum)).Trim()
                if ($expected -notmatch '^[0-9a-f]{64}$' -or (Get-FileHash -LiteralPath $Binary -Algorithm SHA256).Hash -ne $expected) {throw 'SHA-256 verification failed.'}
            } else {
            $release='https://github.com/RuoJi6/AegisHook/releases'
            $checksums=Join-Path $temporary 'SHA256SUMS.txt'
            if ($Version -eq 'latest') {
                Invoke-WebRequest -UseBasicParsing -Uri "$release/latest/download/SHA256SUMS.txt" -OutFile $checksums
                $matching=@(Get-Content -LiteralPath $checksums | Where-Object {$_ -match '^([0-9a-f]{64})\s+\*?aegishook_(v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?)_windows_amd64\.zip$'})
                if ($matching.Count -ne 1) {throw 'Cannot identify the latest Windows release.'}
                $null=$matching[0] -match 'aegishook_(v.+)_windows_amd64\.zip$'
                $Version=$Matches[1]
            } else {
                if ($Version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$') {throw 'Invalid version.'}
                Invoke-WebRequest -UseBasicParsing -Uri "$release/download/$Version/SHA256SUMS.txt" -OutFile $checksums
            }
            $package="aegishook_${Version}_windows_amd64"
            $archive=Join-Path $temporary "$package.zip"
            Invoke-WebRequest -UseBasicParsing -Uri "$release/download/$Version/$package.zip" -OutFile $archive
            $pattern='^([0-9a-f]{64})\s+\*?'+[regex]::Escape("$package.zip")+'$'
            $matching=@(Get-Content -LiteralPath $checksums | Where-Object {$_ -match $pattern})
            if ($matching.Count -ne 1) {throw 'Missing or duplicate checksum.'}
            $null=$matching[0] -match $pattern
            if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash -ne $Matches[1]) {throw 'SHA-256 verification failed.'}
            Add-Type -AssemblyName System.IO.Compression.FileSystem
            $zip=[IO.Compression.ZipFile]::OpenRead($archive)
            try {
                $entry=$zip.GetEntry("$package/aegishook.exe")
                if (-not $entry) {throw 'Executable missing from archive.'}
                $Binary=Join-Path $temporary 'aegishook.exe'
                [IO.Compression.ZipFileExtensions]::ExtractToFile($entry,$Binary,$false)
            } finally { $zip.Dispose() }
            }
        }
        Assert-RegularFile $Binary
        & $Binary client install -h | Out-Null
        if ($LASTEXITCODE -ne 0) {throw 'This version does not support client installation.'}
        New-Item -ItemType Directory -Path $ClientDir -Force | Out-Null
        Protect-ClientDirectory $ClientDir
        if ((Test-Path -LiteralPath $executable) -or (Test-Path -LiteralPath $marker)) {throw 'Existing executable or ownership record; nothing overwritten.'}
        [IO.File]::Copy([IO.Path]::GetFullPath($Binary),$executable,$false)
        [IO.File]::WriteAllText($marker,(Get-FileHash -LiteralPath $executable -Algorithm SHA256).Hash)
    }
    Assert-RegularFile $executable
    Assert-RegularFile $marker
    if ((Get-FileHash -LiteralPath $executable -Algorithm SHA256).Hash -ne ([IO.File]::ReadAllText($marker)).Trim()) {throw 'Client executable was modified.'}
    $arguments=@('client',$Action,'--client-dir',$ClientDir,'--agent',$Agent,'--scope',$Scope)
    if ($Project) {$arguments+=@('--project',$Project)}
    if ($Action -eq 'install') {
        $arguments+=@('--endpoint',$Endpoint)
        if ($SwitchScope) {$arguments+='--switch'}
        if ($CodeFile) {
            & $executable @arguments --code-file $CodeFile
        } else {
            & $executable @arguments
        }
    } else { & $executable @arguments }
    if ($LASTEXITCODE -ne 0) {throw 'Hook operation failed. Existing configuration has been retained.'}
    if ($Action -eq 'uninstall' -and -not (Test-Path -LiteralPath $manifest)) {
        Remove-Item -LiteralPath $executable,$marker
        Write-Output 'All Hooks removed. The lock file and unowned files are retained.'
    }
} finally {
    if ($temporary -and (Test-Path -LiteralPath $temporary)) {Remove-Item -LiteralPath $temporary -Recurse -Force}
}
