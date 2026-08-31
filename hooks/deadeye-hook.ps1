# Resolves the deadeye binary and forwards this hook invocation to it.
# Fail-open per INV-5: any resolution or execution failure prints {} and
# exits 0.
#
# Binary resolution mirrors deadeye-hook.sh: a `deadeye` on PATH is used only
# when it is at least the plugin's version; a STALE PATH binary defers to the
# managed %USERPROFILE%\.deadeye\bin\deadeye.exe so plugin updates aren't
# shadowed. NOTE: Windows self-bootstrap (auto-downloading the managed binary,
# as hooks/bootstrap.sh does on macOS/Linux) is still not implemented -- until
# then, Windows users must put deadeye.exe on PATH or at that managed path
# themselves. deadeye's own version-skew warning (see /deadeye-status) flags a
# stale binary when it happens.
param([string]$Event)
$ErrorActionPreference = 'SilentlyContinue'

function Get-PluginVersion {
    $pj = "$env:CLAUDE_PLUGIN_ROOT\.claude-plugin\plugin.json"
    if ($env:CLAUDE_PLUGIN_ROOT -and (Test-Path $pj)) {
        try { return (Get-Content $pj -Raw | ConvertFrom-Json).version } catch { return $null }
    }
    return $null
}

function Get-BinVersion([string]$bin) {
    try { $o = & $bin version 2>$null; if ($o) { return ($o -split '\s+')[1] } } catch {}
    return $null
}

function ConvertTo-VerParts([string]$s) {
    $out = @(0, 0, 0)
    if ($s) {
        $s = ($s -replace '^v', '') -replace '[^0-9.].*$', ''
        $parts = $s -split '\.'
        for ($i = 0; $i -lt 3; $i++) {
            if ($i -lt $parts.Count -and $parts[$i] -match '^\d+$') { $out[$i] = [int]$parts[$i] }
        }
    }
    return , $out
}

function Test-VerGE([string]$a, [string]$b) {
    $pa = ConvertTo-VerParts $a
    $pb = ConvertTo-VerParts $b
    for ($i = 0; $i -lt 3; $i++) {
        if ($pa[$i] -gt $pb[$i]) { return $true }
        if ($pa[$i] -lt $pb[$i]) { return $false }
    }
    return $true
}

$pv = Get-PluginVersion
$pathCmd = Get-Command deadeye -ErrorAction SilentlyContinue
$pathBin = if ($pathCmd) { $pathCmd.Source } else { $null }
$managed = "$env:USERPROFILE\.deadeye\bin\deadeye.exe"
$hasManaged = Test-Path $managed

$binPath = $null
if ($pathBin) {
    if ((-not $pv) -or (Test-VerGE (Get-BinVersion $pathBin) $pv)) {
        $binPath = $pathBin          # no plugin context, or current/newer build -- stays in charge
    } elseif ($hasManaged) {
        $binPath = $managed          # stale PATH binary -> managed one takes over
    } else {
        $binPath = $pathBin          # stale, but nothing managed present
    }
} elseif ($hasManaged) {
    $binPath = $managed
}

if (-not $binPath) {
    Write-Output '{}'
    exit 0
}

try {
    $out = & $binPath hook $Event 2>$null
} catch {
    $out = $null
}

if ([string]::IsNullOrWhiteSpace($out)) {
    Write-Output '{}'
} else {
    Write-Output $out
}
