@echo off
rem Installs the ReCam test camera into Program Files, where Windows' camera service can load it.
net session >nul 2>&1
if errorlevel 1 (
  echo Please right-click install.cmd and choose "Run as administrator".
  pause
  exit /b 1
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
