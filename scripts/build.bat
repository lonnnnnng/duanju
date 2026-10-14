@echo off
setlocal
set "GOTOOLCHAIN=local"
chcp 65001 >nul
cd /d "%~dp0.."
go env -w GOPROXY=https://goproxy.cn,direct
if errorlevel 1 exit /b 1
go env -w GOSUMDB=off
if errorlevel 1 exit /b 1
if not exist dist mkdir dist
if "%VERSION%"=="" set "VERSION=dev"
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags="-s -w -X duanju/internal/app.Version=%VERSION%" -o dist\duanju-%VERSION%-windows-amd64.exe .
if errorlevel 1 exit /b 1
echo 已生成 dist\duanju-%VERSION%-windows-amd64.exe
