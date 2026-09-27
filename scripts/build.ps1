param(
    [ValidateSet('linux', 'darwin', 'windows')]
    [string]$TargetOS,
    [ValidateSet('amd64', 'arm64')]
    [string]$TargetArch,
    [switch]$AllTargets,
    [switch]$BackendOnly,
    [switch]$ForDocker
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    if (-not $BackendOnly) {
        & npm run build
        if ($LASTEXITCODE -ne 0) {
            throw "Frontend build failed with exit code $LASTEXITCODE."
        }
    }

    if ($ForDocker -and $TargetOS -and $TargetOS -ne 'linux') {
        throw '-ForDocker requires -TargetOS linux.'
    }

    if ($ForDocker) {
        $TargetOS = 'linux'
        if (-not $TargetArch) {
            $TargetArch = 'amd64'
        }
    }
    elseif (-not $TargetOS) {
        if ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Windows)) {
            $TargetOS = 'windows'
        }
        elseif ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::OSX)) {
            $TargetOS = 'darwin'
        }
        else {
            $TargetOS = 'linux'
        }
    }

    if (-not $TargetArch) {
        $hostArchitecture = [System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString()
        $TargetArch = switch ($hostArchitecture) {
            'X64' { 'amd64' }
            'Arm64' { 'arm64' }
            default { throw "Unsupported host architecture: $hostArchitecture." }
        }
    }

    $targets = if ($AllTargets) {
        foreach ($os in @('linux', 'darwin', 'windows')) {
            foreach ($arch in @('amd64', 'arm64')) {
                [PSCustomObject]@{ OS = $os; Arch = $arch }
            }
        }
    }
    else {
        @([PSCustomObject]@{ OS = $TargetOS; Arch = $TargetArch })
    }

    New-Item -ItemType Directory -Force -Path (Join-Path $repoRoot 'dist') | Out-Null

    if ($ForDocker) {
        if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
            throw 'Docker is required with -ForDocker.'
        }

        foreach ($target in $targets) {
            $extension = if ($target.OS -eq 'windows') { '.exe' } else { '' }
            $name = "gpx_marketplace_profit_$($target.OS)_$($target.Arch)$extension"
            $containerCommand = "CGO_ENABLED=0 GOOS=$($target.OS) GOARCH=$($target.Arch) go build -trimpath -o dist/$name ./pkg && { if [ '$($target.OS)' != 'windows' ]; then chmod 755 dist/$name; fi; }"
            & docker run --rm --mount "type=bind,source=$repoRoot,target=/workspace" -w /workspace golang:1.23.5 sh -c $containerCommand
            if ($LASTEXITCODE -ne 0) {
                throw "Docker backend build failed for $($target.OS)/$($target.Arch) with exit code $LASTEXITCODE."
            }
        }
    }
    else {
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
            throw 'Go 1.23.5 or newer is required. Install Go or use -ForDocker with Docker Desktop.'
        }

        foreach ($target in $targets) {
            $extension = if ($target.OS -eq 'windows') { '.exe' } else { '' }
            $output = Join-Path $repoRoot "dist/gpx_marketplace_profit_$($target.OS)_$($target.Arch)$extension"
            $env:CGO_ENABLED = '0'
            $env:GOOS = $target.OS
            $env:GOARCH = $target.Arch
            & go build -trimpath -o $output ./pkg
            if ($LASTEXITCODE -ne 0) {
                throw "Backend build failed for $($target.OS)/$($target.Arch) with exit code $LASTEXITCODE."
            }
        }
    }
}
finally {
    Pop-Location
}
