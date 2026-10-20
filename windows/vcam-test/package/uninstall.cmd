@echo off
rem Removes the ReCam test camera. Close ReCamTestCamera.exe first.
net session >nul 2>&1
if errorlevel 1 (
  echo Please right-click uninstall.cmd and choose "Run as administrator".
  pause
  exit /b 1
)
set "DEST=%ProgramFiles%\ReCam\TestCamera"
taskkill /im ReCamTestCamera.exe /f >nul 2>&1
if exist "%DEST%\ReCamTestSource.dll" regsvr32 /u /s "%DEST%\ReCamTestSource.dll"
rmdir /s /q "%DEST%" 2>nul
rmdir "%ProgramFiles%\ReCam" 2>nul
echo The test camera was removed.
pause
