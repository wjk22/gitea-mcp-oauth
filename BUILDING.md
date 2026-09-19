# Building gitea-mcp on Windows

This project includes PowerShell and batch scripts to build the gitea-mcp application on Windows systems.

## Prerequisites

- Go 1.27 or later
- Git (for version information)
- PowerShell 5.1 or later (included with Windows 10/11)

## Build Scripts

### PowerShell Script (`build.ps1`)

The main build script that replicates all Makefile functionality:

```powershell
# Show help
.\build.ps1 help

# Build the application
.\build.ps1 build

# Install the application
.\build.ps1 install

# Clean build artifacts
.\build.ps1 clean

# Run in development mode (hot reload)
.\build.ps1 dev

# Update vendor dependencies
.\build.ps1 vendor
```

### Batch File Wrapper (`build.bat`)

A simple wrapper to run the PowerShell script:

```cmd
# Run with default help target
build.bat

# Run specific target
build.bat build
build.bat install
```

## Available Targets

- **help** - Print help message
- **build** - Build the application executable
- **install** - Build and install to GOPATH/bin
- **uninstall** - Remove executable from GOPATH/bin
- **clean** - Remove build artifacts
- **air** - Install air for hot reload development
- **dev** - Run with hot reload development
- **vendor** - Tidy and verify Go module dependencies

## Output

The build process creates `gitea-mcp.exe` in the project directory.
