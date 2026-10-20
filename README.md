# ReCam

**iPhone webcam for Discord on Windows.**

Turn your iPhone into a virtual webcam for Discord and other desktop applications on Windows and Linux.

The goal is to provide a lightweight, open-source alternative to paid webcam apps by streaming your iPhone's camera to your PC and exposing it as a standard virtual camera.

> **Status: Research / Early Development**
>
> This project is currently in the design and validation stage. The complete streaming pipeline and Discord compatibility have not yet been verified.

## Overview

Apple's Continuity Camera makes an iPhone a webcam for a Mac, but does not provide the same built-in experience on Windows or Linux. This project aims to bridge that gap without requiring a Mac or a proprietary webcam subscription.

The intended result is simple: open the camera on your iPhone, connect it to your PC, and select it in Discord's camera settings like any other webcam.

### Planned features

- **iPhone camera streaming** — Send live video from Safari to your PC over WebRTC.
- **System-wide virtual webcam** — Make the iPhone feed available to Discord, OBS, Zoom and other compatible applications.
- **Windows 11 support** — Use a Media Foundation virtual camera, with a DirectShow fallback if required.
- **Linux support** — Integrate with V4L2 using `v4l2loopback`.
- **Low-latency video** — Hardware-encoded H.264 streaming with minimal buffering.
- **720p at 30 FPS** — Target a practical balance of image quality, latency and performance.
- **Easy pairing** — Connect the phone to the PC using a browser-based interface and a pairing QR code.
- **Native iOS client (planned)** — Explore background operation and USB connectivity beyond the limitations of Safari.

These are development goals, not a list of features already available in a released build.

## How it works

The project is designed around three components:

```text
┌─────────────────────────┐
│         iPhone          │
│                         │
│  Safari / Native iOS    │
│  Camera Capture         │
│  H.264 Encoding         │
└────────────┬────────────┘
             │
             │ WebRTC over Wi-Fi
             │
             ▼
┌─────────────────────────┐
│           PC            │
│                         │
│  Receiver & Decoder     │
│  Frame Processing       │
│  Virtual Camera Output  │
└────────────┬────────────┘
             │
             ▼
┌─────────────────────────┐
│    Desktop Applications │
│                         │
│  Discord · OBS · Zoom   │
└─────────────────────────┘
```

### 1. iPhone sender

The initial version is planned as a browser-based client that captures the camera using `getUserMedia()` and sends H.264 video through WebRTC.

Because iOS restricts background browser capture, the phone must remain awake with the camera page active. The sender also requires a trusted HTTPS connection.

A native Swift application is a potential later improvement.

### 2. PC receiver

The receiver establishes the connection, receives the encoded video, decodes the frames and prepares them for the virtual camera.

The implementation is planned around Go and Pion WebRTC, with FFmpeg libraries for video decoding and frame conversion.

### 3. Virtual camera

The receiver exposes the decoded video through the operating system's camera interface.

| Platform | Planned implementation |
|---|---|
| Windows 11 | Media Foundation virtual camera |
| Windows fallback | 64-bit DirectShow source filter |
| Linux | V4L2 virtual device via `v4l2loopback` |

Compatibility with Discord's official desktop client is a primary validation target.

## Technical targets

| Property | Target |
|---|---|
| Resolution | 1280 × 720 |
| Frame rate | 30 FPS |
| Video codec | H.264 / AVC |
| Transport | WebRTC over a local network |
| Primary connection | Wi-Fi |
| Video orientation | Landscape output |
| Audio | Out of scope for the initial version |

The initial goal is a reliable 720p stream, rather than pursuing unnecessarily high resolutions or frame rates.

## Requirements

The final requirements will depend on the platform and implementation.

### iPhone

- iOS 18.4 or later for the planned browser-based implementation.
- Safari or another iOS browser using WebKit.
- Camera permissions enabled.
- A trusted HTTPS connection to the receiver.
- The device must remain awake while using browser-based capture.

Newer iOS versions are preferred for the planned feature set.

### Windows

- Windows 11, build 22000 or later, for the primary Media Foundation implementation.
- Camera privacy permissions enabled for desktop applications.
- Network access between the phone and PC.
- A compatible virtual camera implementation.

A DirectShow fallback may be necessary depending on the results of compatibility testing.

### Linux

- A supported Linux distribution and kernel.
- `v4l2loopback` and its associated utilities.
- Permission to access the virtual video device.
- Network connectivity between the phone and PC.

Kernel updates may require rebuilding the `v4l2loopback` module.

## Development roadmap

The project is being developed in stages, prioritising the highest-risk compatibility questions first.

- [ ] Validate a Windows virtual camera with Discord.
- [ ] Confirm Windows virtual camera behaviour across reboots and multiple applications.
- [ ] Validate the Linux V4L2 implementation with Discord.
- [ ] Build the receiver and establish a working WebRTC connection.
- [ ] Implement the iPhone browser-based sender.
- [ ] Connect the complete pipeline from iPhone to Discord.
- [ ] Add pairing, certificate setup and connection diagnostics.
- [ ] Improve latency, reconnection and stream stability.
- [ ] Package the Windows application and installer.
- [ ] Implement and document Linux installation.
- [ ] Investigate a native Swift sender and USB connectivity.
- [ ] Evaluate audio support separately.

The first major milestone is a working 720p, 30 FPS camera feed in Discord's official desktop application.

## Current limitations

The design has several constraints that influence the implementation:

- **HTTPS is required:** Safari needs a trusted secure context to access the camera.
- **Browser background restrictions:** The initial sender cannot reliably keep capturing when the page is hidden or the phone is locked.
- **Virtual camera compatibility:** Windows' Media Foundation camera must be tested with Discord; a fallback may be required.
- **Linux kernel integration:** `v4l2loopback` may need rebuilding after kernel updates.
- **Local network requirements:** Firewalls, VPNs and Wi-Fi client isolation can prevent the devices from communicating.
- **Native iOS distribution:** A native client introduces Apple signing and build-environment requirements.
- **No bundled microphone:** The initial version is focused on video; users can continue using their PC microphone.

## Security and privacy

The intended architecture keeps video transmission local to the user's network.

- No cloud video relay is planned.
- The receiver should accept connections only as needed and use appropriately restricted firewall rules.
- HTTPS certificate handling and pairing must be implemented carefully.
- Video frames should not be retained or recorded by default.
- Network access and camera permissions remain under the user's control.

These are design objectives; verify the implementation and network behaviour before relying on them.

## Contributing

Contributions, testing and implementation feedback are welcome.

Areas where help would be particularly useful include:

- Windows Media Foundation virtual cameras.
- DirectShow and WebRTC capture compatibility.
- Linux V4L2 and `v4l2loopback`.
- WebRTC latency and H.264 decoding.
- Safari camera capture and HTTPS certificate handling.
- Native iOS video capture and USB transport.

Please document your operating system, application version and reproduction steps when reporting compatibility issues. See [CONTRIBUTING.md](CONTRIBUTING.md) for details, and [SECURITY.md](SECURITY.md) for reporting security problems privately.

## License

ReCam is licensed under the [GNU General Public License v3.0](LICENSE).

---

**The goal:** Make an iPhone work as a proper, system-wide webcam on Windows and Linux, without requiring a Mac or a paid webcam application.
