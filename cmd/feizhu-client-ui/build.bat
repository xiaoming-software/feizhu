@echo off
setlocal
REM 飞猪 GUI 客户端 — Windows 本机编译入口（双击或 cmd 均可）
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" %*
exit /b %ERRORLEVEL%
