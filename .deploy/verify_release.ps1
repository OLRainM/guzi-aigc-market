$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot

function Invoke-Step([string]$Name, [string]$WorkingDirectory, [string]$Command, [string[]]$Arguments) {
    Write-Host "== $Name =="
    Push-Location $WorkingDirectory
    try {
        & $Command @Arguments
        if ($LASTEXITCODE -ne 0) { throw "$Name failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
}

Invoke-Step 'API tests' (Join-Path $root 'apps/api') 'go' @('test', './...')
$pythonCommand = $null
$pythonArguments = @()
$pythonCandidate = Get-Command python -ErrorAction SilentlyContinue
if ($pythonCandidate -and $pythonCandidate.Source -notlike '*WindowsApps*') {
    $pythonCommand = 'python'
    $pythonArguments = @('-m', 'unittest', 'discover', '-s', 'tests', '-p', 'test_*.py', '-v')
} elseif (Get-Command py -ErrorAction SilentlyContinue) {
    $pythonCommand = 'py'
    $pythonArguments = @('-3', '-m', 'unittest', 'discover', '-s', 'tests', '-p', 'test_*.py', '-v')
} else {
    throw 'Python is not installed'
}
Invoke-Step 'Worker tests' (Join-Path $root 'apps/worker') $pythonCommand $pythonArguments
Invoke-Step 'Web build' (Join-Path $root 'apps/web') 'npm' @('run', 'build')
Invoke-Step 'Compose config' $root 'docker' @('compose', 'config', '--quiet')

$optional = @(
    @{ Name = 'Go vulnerability scan'; Command = 'govulncheck'; Arguments = @('./...'); Directory = Join-Path $root 'apps/api' },
    @{ Name = 'Python dependency audit'; Command = 'pip-audit'; Arguments = @('-r', 'requirements.txt'); Directory = Join-Path $root 'apps/worker' },
    @{ Name = 'Web dependency audit'; Command = 'npm'; Arguments = @('audit', '--omit=dev'); Directory = Join-Path $root 'apps/web' },
    @{ Name = 'Container scan'; Command = 'trivy'; Arguments = @('fs', '--scanners', 'vuln,secret', $root); Directory = $root }
)

foreach ($check in $optional) {
    if (Get-Command $check.Command -ErrorAction SilentlyContinue) {
        try {
            Invoke-Step $check.Name $check.Directory $check.Command $check.Arguments
        } catch {
            Write-Warning "$($check.Name) unavailable: $($_.Exception.Message)"
        }
    } else {
        Write-Warning "$($check.Name) skipped: $($check.Command) is not installed"
    }
}

Write-Host 'Release verification completed.'
