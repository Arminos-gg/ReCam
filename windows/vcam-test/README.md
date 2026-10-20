# Windows test camera

A Media Foundation virtual camera that shows a test pattern. It answers the first
open question in the plan: does Discord's desktop app list and play a Windows 11
virtual camera created with `MFCreateVirtualCamera`?

It is based on [VCamSample](https://github.com/smourier/VCamSample) by Simon Mourier
(MIT, see `LICENSE-VCamSample`), copied at commit `58b6d4b`. Changes from upstream:

- Offers NV12 and YUY2 at 1280x720 and 1920x1080, 30 fps (720p first). Upstream
  offered RGB32 and NV12 at 1280x960.
- Renders on the CPU only, so YUY2 works and the path matches the later receiver.
- Draws colour bars and a moving bar instead of per-pixel HSL blocks, which keeps
  1080p cheap on the CPU. The overlay shows format, resolution, frame number and fps.
- Own CLSID and the camera name "ReCam Test Camera".
- Fixes in upstream code: `SetStreamState` compared with `=` instead of `==`,
  `MediaSource::Start` read the stream state with the wrong index, a restart passed a
  null media type to the sample allocator, and NV12's default stride was 1.5x the width.

## Getting a build

GitHub Actions builds it on every push that touches this folder
(`.github/workflows/windows-vcam-test.yml`). Download the `ReCam-test-camera`
artifact from the workflow run and follow `package/README.txt`.

To build locally, open `VCamSample.sln` in Visual Studio 2022 or 2026 and build
Release x64.
