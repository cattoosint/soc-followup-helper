@echo off
cd /d "%~dp0"
rem Always opens the GUI. Drag a CSV/XLSX onto this file to preload it.
start "" pythonw soc_gui.py %1
