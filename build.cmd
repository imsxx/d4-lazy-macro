@echo off
setlocal
chcp 65001 >nul
cd /d "%~dp0"

echo [1/2] 构建 dist\diablolazymacro.exe ...
go build -trimpath -ldflags "-s -w -H windowsgui" -o "dist\diablolazymacro.exe" .
if errorlevel 1 (
    echo 构建失败：请确认已安装 Go 并在 PATH 中。
    if /i not "%~1"=="--no-pause" pause
    exit /b 1
)

echo [2/2] 完成：%~dp0dist\diablolazymacro.exe
echo       桌面快捷方式若已创建会继续指向该文件，无需重建。
if /i not "%~1"=="--no-pause" pause
