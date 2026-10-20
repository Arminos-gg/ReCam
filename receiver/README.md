# ReCam receiver

Receives the iPhone camera from the ReCam page in Safari over WebRTC and shows it in a
preview page on the PC. The next step feeds the same stream into the virtual camera.

- **Phone side:** a page served by the receiver (`web/index.html`, `web/sender.js`). Safari
  captures the camera at 1280x720, 30 fps, encodes H.264 in hardware and sends it with
  WHIP. It keeps the screen awake and reconnects on its own after the phone locks or the
  Wi-Fi drops.
- **PC side:** Go with [Pion WebRTC](https://github.com/pion/webrtc). Everything runs on
  one port: HTTPS on TCP 8443 for the page, WebRTC on UDP 8443. Lost packets are
  re-requested (NACK), and nothing is buffered on the PC beyond what the preview needs.
- **Preview:** `http://127.0.0.1:8080/view.html`, only reachable from the PC itself. It
  shows the QR code until the phone connects, then the video with frame rate, bitrate,
  jitter buffer, decode time and packet loss.

## Trying it on Windows

1. Download the `ReCam-receiver-windows` artifact from the latest **Receiver** workflow
   run and extract it.
2. Double-click `recam-receiver.exe`. When Windows Defender Firewall asks, allow it on
   **private networks**. The preview page opens in your browser and shows a QR code.
3. Make sure the iPhone is on the same Wi-Fi as the PC, then scan the QR code with the
   iPhone camera and open the link in Safari.
4. Safari warns that the connection is not private, because the certificate comes from
   the receiver itself. Either:
   - **Quick:** tap *Show Details*, then *visit this website*, and confirm; or
   - **Once and for all:** open `https://<PC address>:8443/recam-ca.crt` on the phone,
     install the downloaded profile under *Settings › General › VPN & Device Management*,
     then turn on *ReCam Local CA* under *Settings › General › About › Certificate Trust
     Settings*. The address is printed in the receiver's window.
5. Turn the phone sideways, press **Start** and allow camera access.

The phone keeps sending only while the page is on screen. Locking the phone or switching
apps pauses the camera; coming back resumes it.

Options: `-port` (default 8443), `-preview-port` (default 8080), `-ip` to choose which
address of the PC goes into the QR code, `-no-browser`, `-data` for where the
certificates and pairing code are kept (default `%AppData%\ReCam`).

## Developing

```
go test -race ./...       # includes an end-to-end test: WHIP in, WHEP out, phone reconnect
go run . -no-browser
```

`testdata/testsrc-320x180-30fps.h264` is two seconds of FFmpeg's `testsrc2` pattern,
H.264 Constrained Baseline, used by the end-to-end test.
