@echo off
rem stowage downloader for Windows CMD.
rem
rem Bootstraps install.ps1 via PowerShell so the same logic handles both
rem PowerShell (irm | iex) and CMD users.
rem
rem   curl -fsSL https://stowage.dev/install.cmd -o install.cmd ^&^& install.cmd ^&^& del install.cmd
rem
rem Environment overrides are passed straight through to the PowerShell script:
rem   STOWAGE_VERSION, STOWAGE_REPO, STOWAGE_RELEASE_BASE, STOWAGE_NO_RUN

setlocal enableextensions

set "STOWAGE_PS1_URL=%STOWAGE_PS1_URL%"
if "%STOWAGE_PS1_URL%"=="" set "STOWAGE_PS1_URL=https://stowage.dev/install.ps1"

set "PS_ARGS="
:collect
if "%~1"=="" goto run
set "PS_ARGS=%PS_ARGS% '%~1'"
shift
goto collect

:run
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command ^
  "& { $script = (Invoke-WebRequest -UseBasicParsing -Uri $env:STOWAGE_PS1_URL).Content; $sb = [scriptblock]::Create($script); & $sb %PS_ARGS% }"
set "ERR=%ERRORLEVEL%"

endlocal & exit /b %ERR%
