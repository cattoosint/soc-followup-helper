@echo off
setlocal enabledelayedexpansion
title SOC Nightshift Follow-up - Setup
cd /d "%~dp0"

rem ============================================================
rem  If the network needs a proxy or an internal package mirror,
rem  remove the "rem " from the lines below and fill them in.
rem ============================================================
rem set HTTPS_PROXY=http://proxy.company.com:8080
rem set PIP_INDEX_URL=https://artifactory.company.com/api/pypi/pypi-remote/simple
rem set PIP_TRUSTED_HOST=artifactory.company.com

echo ============================================================
echo   SOC Nightshift Follow-up  -  setup
echo ============================================================
echo.

rem ---------- 1. find a usable Python --------------------------
set PYCMD=
for %%C in ("python" "py -3") do (
    if not defined PYCMD (
        %%~C -c "import sys; raise SystemExit(0 if sys.version_info>=(3,10) else 1)" >nul 2>&1
        if !errorlevel! equ 0 set "PYCMD=%%~C"
    )
)

if not defined PYCMD (
    echo Python 3.10+ was not found on this machine.
    echo.
    where winget >nul 2>&1
    if !errorlevel! equ 0 (
        echo Trying to install Python for your user account via winget...
        echo.
        winget install -e --id Python.Python.3.12 --scope user ^
              --accept-package-agreements --accept-source-agreements
        echo.
        echo If that succeeded, CLOSE this window and run install.bat again
        echo so Windows picks up the new PATH.
    ) else (
        echo winget is not available here, so Python must be installed the
        echo normal way for this machine:
        echo.
        echo   - Company Portal / Software Center, or
        echo   - https://www.python.org/downloads/  ^(tick "Add python.exe to PATH"^)
        echo.
        echo Then run install.bat again.
    )
    echo.
    pause
    exit /b 1
)

for /f "delims=" %%V in ('%PYCMD% -c "import sys;print(sys.version.split()[0])"') do set PYVER=%%V
echo Found Python %PYVER%   ^(%PYCMD%^)
echo.

rem ---------- 2. install the libraries -------------------------
echo Installing required packages...
echo.
%PYCMD% -m pip install --upgrade pip
%PYCMD% -m pip install -r requirements.txt
if errorlevel 1 (
    echo.
    echo ------------------------------------------------------------
    echo   Package installation FAILED.
    echo   If this machine needs a proxy or an internal package
    echo   mirror, open install.bat in Notepad and fill in the
    echo   settings at the top, then run it again.
    echo ------------------------------------------------------------
    echo.
    pause
    exit /b 1
)

rem ---------- 3. check it actually imports ---------------------
echo.
echo Checking the install...
%PYCMD% -c "import selenium, openpyxl, psutil, tkinter; print('  selenium ', selenium.__version__); print('  openpyxl ', openpyxl.__version__); print('  psutil   ', psutil.__version__); print('  tkinter   ok')"
if errorlevel 1 (
    echo.
    echo Something is still missing - see the error above.
    pause
    exit /b 1
)

echo.
echo ============================================================
echo   Setup complete.
echo.
echo   Start the tool with:  run_followup.bat
echo   ^(or:  %PYCMD% soc_gui.py^)
echo.
echo   First run opens Outlook so you can sign in once - choose
echo   "Yes" at "Stay signed in?" and it is remembered after that.
echo ============================================================
echo.
pause
