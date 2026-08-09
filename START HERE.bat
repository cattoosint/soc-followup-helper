@echo off
title SOC Nightshift Follow-up
cd /d "%~dp0"

rem Nothing to install - the libraries live in the libs folder next to this.

set PY=
python -c "import sys; raise SystemExit(0 if sys.version_info>=(3,10) else 1)" >nul 2>&1
if not errorlevel 1 set PY=python
if not defined PY (
    py -3 -c "import sys; raise SystemExit(0 if sys.version_info>=(3,10) else 1)" >nul 2>&1
    if not errorlevel 1 set PY=py -3
)

if not defined PY (
    echo.
    echo Python 3.10 or newer was not found.
    echo Install it from Company Portal / Software Center, or python.org
    echo ^(tick "Add python.exe to PATH"^), then run this again.
    echo.
    pause
    exit /b 1
)

start "" %PY% soc_gui.py
