@echo off
rem Removes the ReCam test camera. Close ReCamTestCamera.exe first.
rem Ask for administrator rights if we don't have them yet, then run again elevated.
net session >nul 2>&1
if errorlevel 1 (
  powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs" || (
    echo Administrator rights are needed. Please allow the prompt and try again.
    pause
  )
  exit /b
)
set "DEST=%ProgramFiles%\ReCam\TestCamera"
taskkill /im ReCamTestCamera.exe /f >nul 2>&1
if exist "%DEST%\ReCamTestSource.dll" regsvr32 /u /s "%DEST%\ReCamTestSource.dll"
rmdir /s /q "%DEST%" 2>nul
rmdir "%ProgramFiles%\ReCam" 2>nul
echo The test camera was removed.
pause
