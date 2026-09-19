#!/usr/bin/env pwsh

# PowerShell build script for gitea-mcp
# Replicates the functionality of the Makefile

param(
    [string]$Target = "help"
)

# Configuration
$EXECUTABLE = "gitea-mcp.exe"
$VERSION = & git describe --tags --always 2>$null | ForEach-Object { $_ -replace '-', '+' -replace '^v', '' }
if (-not $VERSION) { $VERSION = "dev" }
$LDFLAGS = "-X `"main.Version=$VERSION`""

# Colors for output (Windows PowerShell compatible)
$CYAN = "Cyan"
$RESET = "White"

function Write-Header {
    param([string]$Message)
    Write-Host "=== $Message ===" -ForegroundColor Green
}

function Write-Info {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Yellow
}

function Write-Success {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Green
}

function Write-Error {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Red
}

function Get-Help {
    Write-Host "Usage: .\build.ps1 [target]" -ForegroundColor Green
    Write-Host ""
    Write-Host "Targets:" -ForegroundColor Green
    Write-Host ""
    
    Write-Host ("{0,-30}" -f "help") -ForegroundColor Cyan -NoNewline
    Write-Host " Print this help message."
    Write-Host ("{0,-30}" -f "build") -ForegroundColor Cyan -NoNewline
    Write-Host " Build the application."
    Write-Host ("{0,-30}" -f "install") -ForegroundColor Cyan -NoNewline
    Write-Host " Install the application."
    Write-Host ("{0,-30}" -f "uninstall") -ForegroundColor Cyan -NoNewline
    Write-Host " Uninstall the application."
    Write-Host ("{0,-30}" -f "clean") -ForegroundColor Cyan -NoNewline
    Write-Host " Clean the build artifacts."
    Write-Host ("{0,-30}" -f "air") -ForegroundColor Cyan -NoNewline
    Write-Host " Install air for hot reload."
    Write-Host ("{0,-30}" -f "dev") -ForegroundColor Cyan -NoNewline
    Write-Host " Run the application with hot reload."
    Write-Host ("{0,-30}" -f "vendor") -ForegroundColor Cyan -NoNewline
    Write-Host " Tidy and verify module dependencies."
}

function Build-App {
    Write-Header "Building application"
    
    $ldflags = "-s -w $LDFLAGS"
    Write-Info "go build -v -ldflags '$ldflags' -o $EXECUTABLE"
    
    try {
        & go build -v -ldflags $ldflags -o $EXECUTABLE
        if ($LASTEXITCODE -eq 0) {
            Write-Success "Build successful: $EXECUTABLE"
        } else {
            Write-Error "Build failed with exit code: $LASTEXITCODE"
            exit $LASTEXITCODE
        }
    } catch {
        Write-Error "Build failed: $_"
        exit 1
    }
}

function Install-App {
    Write-Header "Installing application"
    
    # First build the application
    Build-App
    
    $GOPATH = $env:GOPATH
    if (-not $GOPATH) {
        $GOPATH = Join-Path $env:USERPROFILE "go"
    }
    
    $installDir = Join-Path $GOPATH "bin"
    $installPath = Join-Path $installDir $EXECUTABLE
    
    Write-Info "Installing $EXECUTABLE to $installPath"
    
    # Create directory if it doesn't exist
    if (-not (Test-Path $installDir)) {
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    }
    
    # Copy the executable
    if (Test-Path $EXECUTABLE) {
        Copy-Item $EXECUTABLE $installPath -Force
        Write-Success "Installed $EXECUTABLE to $installPath"
        Write-Info "Please add $installDir to your PATH if it is not already there."
    } else {
        Write-Error "Executable not found. Please build first."
        exit 1
    }
}

function Uninstall-App {
    Write-Header "Uninstalling application"
    
    $GOPATH = $env:GOPATH
    if (-not $GOPATH) {
        $GOPATH = Join-Path $env:USERPROFILE "go"
    }
    
    $installPath = Join-Path $GOPATH "bin" $EXECUTABLE
    
    Write-Info "Uninstalling $EXECUTABLE from $installPath"
    
    if (Test-Path $installPath) {
        Remove-Item $installPath -Force
        Write-Success "Uninstalled $EXECUTABLE from $installPath"
    } else {
        Write-Warning "$EXECUTABLE not found at $installPath"
    }
}

function Clean-Build {
    Write-Header "Cleaning build artifacts"
    
    Write-Info "Cleaning up $EXECUTABLE"
    
    if (Test-Path $EXECUTABLE) {
        Remove-Item $EXECUTABLE -Force
        Write-Success "Cleaned up $EXECUTABLE"
    } else {
        Write-Warning "$EXECUTABLE not found"
    }
}

function Install-Air {
    Write-Header "Installing air for hot reload"
    
    # Check if air is already installed
    $airPath = Get-Command air -ErrorAction SilentlyContinue
    if ($airPath) {
        Write-Success "air is already installed"
        return
    }
    
    Write-Info "Installing github.com/air-verse/air@latest"
    try {
        & go install github.com/air-verse/air@latest
        if ($LASTEXITCODE -eq 0) {
            Write-Success "air installed successfully"
        } else {
            Write-Error "Failed to install air"
            exit $LASTEXITCODE
        }
    } catch {
        Write-Error "Failed to install air: $_"
        exit 1
    }
}

function Start-Dev {
    Write-Header "Starting development mode with hot reload"
    
    # Install air first
    Install-Air
    
    Write-Info "Starting air with build configuration"
    & air --build.cmd "go build -o $EXECUTABLE" --build.bin "./$EXECUTABLE"
}

function Update-Vendor {
    Write-Header "Tidying and verifying module dependencies"
    
    Write-Info "Running go mod tidy"
    & go mod tidy
    if ($LASTEXITCODE -ne 0) {
        Write-Error "go mod tidy failed"
        exit $LASTEXITCODE
    }
    
    Write-Info "Running go mod verify"
    & go mod verify
    if ($LASTEXITCODE -ne 0) {
        Write-Error "go mod verify failed"
        exit $LASTEXITCODE
    }
    
    Write-Success "Dependencies updated successfully"
}

# Main execution logic
switch ($Target.ToLower()) {
    "help" { Get-Help }
    "build" { Build-App }
    "install" { Install-App }
    "uninstall" { Uninstall-App }
    "clean" { Clean-Build }
    "air" { Install-Air }
    "dev" { Start-Dev }
    "vendor" { Update-Vendor }
    default {
        Write-Error "Unknown target: $Target"
        Write-Host ""
        Get-Help
        exit 1
    }
}
