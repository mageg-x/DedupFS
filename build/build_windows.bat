@echo off
setlocal enabledelayedexpansion

REM Set environment variables
set "CGO_ENABLED=1"
set "CC=gcc"
set "CPATH=C:\Program Files (x86)\WinFsp\inc\fuse"
set "PROJECT_ROOT=%~dp0.."
set "WIN_DIR=%PROJECT_ROOT%\platform\dedupfs_windows"
set "GUI_OUTPUT_NAME=dedupfs.exe"
set "CLI_OUTPUT_NAME=dedupfs-cli.exe"

REM Create build directory if it doesn't exist
if not exist "%PROJECT_ROOT%\build" (
    mkdir "%PROJECT_ROOT%\build"
    if errorlevel 1 (
        echo Error: Failed to create build directory
        exit /b 1
    )
)

echo Cleaning artifacts...
REM Remove previous build files if they exist
if exist "%PROJECT_ROOT%\build\%GUI_OUTPUT_NAME%" (
    del /q "%PROJECT_ROOT%\build\%GUI_OUTPUT_NAME%"
)
if exist "%PROJECT_ROOT%\build\%CLI_OUTPUT_NAME%" (
    del /q "%PROJECT_ROOT%\build\%CLI_OUTPUT_NAME%"
)

REM Build command line version (does not require wails)
echo Building command line version...
cd /d "%WIN_DIR%"
if errorlevel 1 (
    echo Error: Failed to change directory to %WIN_DIR%
    exit /b 1
)

echo Running: go build -tags "windows,console" -o "%PROJECT_ROOT%\build\%CLI_OUTPUT_NAME%" cmd.go
go build -tags "windows,console" -o "%PROJECT_ROOT%\build\%CLI_OUTPUT_NAME%" cmd.go
if errorlevel 1 (
    echo ERROR: Failed to build command line version.
    exit /b 1
)

echo Command line build succeeded! Output: %PROJECT_ROOT%\build\%CLI_OUTPUT_NAME%

REM Build GUI version (requires wails and frontend)
cd /d "%WIN_DIR%"
if errorlevel 1 (
    echo Error: Failed to change directory to %WIN_DIR%
    goto CLI_ONLY
)

REM Check if wails is available
where wails >nul 2>&1
if errorlevel 1 (
    echo Warning: wails not found. Skipping GUI build.
    goto CLI_ONLY
)

REM Check if Node.js is available
where node >nul 2>&1
if errorlevel 1 (
    echo Warning: Node.js not found. Skipping GUI build.
    goto CLI_ONLY
)

echo Building GUI version...

REM Clean frontend dist directory
if exist "%WIN_DIR%\frontend\dist" (
    rmdir /s /q "%WIN_DIR%\frontend\dist"
    if errorlevel 1 (
        echo Warning: Failed to remove frontend dist directory
    )
)

REM Build frontend
cd /d "%WIN_DIR%\frontend"
if errorlevel 1 (
    echo Error: Failed to change directory to frontend
    goto CLI_ONLY
)

REM Install frontend dependencies if needed
if not exist "node_modules" (
    echo Installing frontend dependencies...
    call npm install
    if errorlevel 1 (
        echo Warning: Failed to install frontend dependencies. Skipping GUI build.
        goto CLI_ONLY
    )
)

echo Building frontend...
call npm run build
if errorlevel 1 (
    echo Warning: Failed to build frontend. Skipping GUI build.
    goto CLI_ONLY
)

REM Build GUI executable
cd /d "%WIN_DIR%"
if errorlevel 1 (
    echo Error: Failed to change directory to %WIN_DIR%
    goto CLI_ONLY
)

echo Building GUI executable...
call wails build --clean -tags "windows,gui"
if errorlevel 1 (
    echo Warning: Failed to build GUI version.
    goto CLI_ONLY
)

REM Copy GUI executable to build directory
if exist "%WIN_DIR%\build\bin\%GUI_OUTPUT_NAME%" (
    copy "%WIN_DIR%\build\bin\%GUI_OUTPUT_NAME%" "%PROJECT_ROOT%\build\%GUI_OUTPUT_NAME%" >nul
    if errorlevel 1 (
        echo Warning: Failed to copy GUI executable
    ) else (
        echo GUI build succeeded! Output: %PROJECT_ROOT%\build\%GUI_OUTPUT_NAME%
    )
) else (
    echo Warning: %GUI_OUTPUT_NAME% not found in build output.
)

:CLI_ONLY
REM Display summary
@REM cls
set "gui_status=NOT BUILT"
set "cli_status=NOT BUILT"

if exist "%PROJECT_ROOT%\build\%CLI_OUTPUT_NAME%" (
    set "cli_status=SUCCESS"
)

if exist "%PROJECT_ROOT%\build\%GUI_OUTPUT_NAME%" (
    set "gui_status=SUCCESS"
)

echo ===============================================================================
echo DEDUPFS BUILD SUMMARY
================================================================================
echo Command Line Version: !cli_status!
if "!cli_status!"=="SUCCESS" echo   - Location: %PROJECT_ROOT%\build\%CLI_OUTPUT_NAME%
echo GUI Version: !gui_status!
if "!gui_status!"=="SUCCESS" echo   - Location: %PROJECT_ROOT%\build\%GUI_OUTPUT_NAME%
echo ===============================================================================