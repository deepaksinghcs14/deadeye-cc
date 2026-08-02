# Resolves the deadeye binary and forwards this hook invocation to it.
# Fail-open per INV-5: any resolution or execution failure prints {} and
# exits 0.
#
# NOTE: Windows self-bootstrap (downloading the release binary on first use,
# as hooks/bootstrap.sh does for macOS/Linux) is not implemented yet -- a
# scoped-out follow-up, not silently dropped. Until then, Windows users must
# put `deadeye.exe` on PATH or at %USERPROFILE%\.deadeye\bin\deadeye.exe
# themselves.
param([string]$Event)
$ErrorActionPreference = 'SilentlyContinue'

$cmd = Get-Command deadeye -ErrorAction SilentlyContinue
if ($cmd) {
    $binPath = $cmd.Source
} elseif (Test-Path "$env:USERPROFILE\.deadeye\bin\deadeye.exe") {
    $binPath = "$env:USERPROFILE\.deadeye\bin\deadeye.exe"
} else {
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
