@echo off
rem Installs the ReCam test camera into Program Files, where Windows' camera service can load it.
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
if not exist "%DEST%" mkdir "%DEST%"
copy /y "%~dp0ReCamTestSource.dll" "%DEST%\" >nul || goto failed
copy /y "%~dp0ReCamTestCamera.exe" "%DEST%\" >nul || goto failed
powershell -NoProfile -Command "Get-ChildItem -LiteralPath '%DEST%' | Unblock-File"
regsvr32 /s "%DEST%\ReCamTestSource.dll" || goto failed
echo Installed to %DEST%
echo Starting the test camera. Keep its window open while you test in Discord.
start "" "%DEST%\ReCamTestCamera.exe"
pause
exit /b 0

:failed
echo Installing the test camera failed.
pause
exit /b 1
