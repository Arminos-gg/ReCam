ReCam test camera
=================

This checks whether Discord accepts a Windows 11 virtual camera before the
real iPhone pipeline is built. It shows colour bars, a moving white bar and
the format, resolution and frame rate the app asked for.

1. If Windows marked the downloaded zip as blocked: right-click it,
   Properties, tick "Unblock", OK. Then extract it.
2. Right-click install.cmd and choose "Run as administrator".
   A window says "ReCam Test Camera is running". Leave it open.
3. Open the Windows Camera app. You should see the test pattern.
4. Open Discord > User Settings > Voice & Video. Under Camera pick
   "ReCam Test Camera" and press Test Video.
5. Note what you see: is the camera listed, does the picture move,
   and which Format, Resolution and Fps does the overlay show?
6. Press Close in the test camera window, then run uninstall.cmd
   as administrator to remove everything.
