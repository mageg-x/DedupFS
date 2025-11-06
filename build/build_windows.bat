@echo off
setlocal

set CGO_ENABLED=1
set CC=gcc
set CPATH=C:\Program Files (x86)\WinFsp\inc\fuse
set PROJECT_ROOT=%~dp0..
set WIN_DIR=%PROJECT_ROOT%\platform\dedupfs_windows
set OUTPUT_NAME=dedupfs.exe

cd /d "%WIN_DIR%" || exit /b 1

where wails >nul 2>&1 || (
    echo Error: wails not found.
    exit /b 1
)

node -v >nul 2>&1 || (
    echo Error: Node.js not found.
    exit /b 1
)

echo Cleaning artifacts...
@REM if exist "%WIN_DIR%\build" rmdir /s /q "%WIN_DIR%\build"
if exist "%WIN_DIR%\frontend\dist" rmdir /s /q "%WIN_DIR%\frontend\dist"
if exist "%PROJECT_ROOT%\build\%OUTPUT_NAME%" del /q "%PROJECT_ROOT%\build\%OUTPUT_NAME%"

if not exist "%PROJECT_ROOT%\build" mkdir "%PROJECT_ROOT%\build"

cd /d "%WIN_DIR%\frontend" || exit /b 1

if not exist "node_modules" (
    echo Installing frontend dependencies...
    call npm install
    if errorlevel 1 exit /b 1
)

echo Building frontend...
call npm run build
if errorlevel 1 exit /b 1

cd /d "%WIN_DIR%" || exit /b 1

echo Building executable...
call wails build --clean
if errorlevel 1 exit /b 1

if exist "%WIN_DIR%\build\bin\%OUTPUT_NAME%" (
    copy "%WIN_DIR%\build\bin\%OUTPUT_NAME%" "%PROJECT_ROOT%\build\%OUTPUT_NAME%" >nul
    echo Build succeeded! Output: %PROJECT_ROOT%\build\%OUTPUT_NAME%
    
    echo Cleaning temporary build directories...
    @REM if exist "%WIN_DIR%\build" rmdir /s /q "%WIN_DIR%\build"
    @REM if exist "%WIN_DIR%\frontend" rmdir /s /q "%WIN_DIR%\frontend"
    echo Cleanup completed.
) else (
    echo ERROR: %OUTPUT_NAME% not found in build output.
    dir "%WIN_DIR%\build\bin" 2>nul
    exit /b 1
)