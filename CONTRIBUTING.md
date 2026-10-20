# Contributing to ReCam

Thanks for your interest in ReCam. The project is at an early stage, so testing and compatibility reports are just as valuable as code.

## Reporting problems

Use the [issue templates](https://github.com/Arminos-gg/ReCam/issues/new/choose). Always include your Windows version, iPhone model and iOS version, and the application you tested (Discord, OBS, Windows Camera and so on).

Questions and ideas belong in [Discussions](https://github.com/Arminos-gg/ReCam/discussions). Security problems are reported privately, see [SECURITY.md](SECURITY.md).

## Making changes

1. Fork the repository and create a branch from `main`.
2. Keep each pull request focused on one change.
3. Describe how you tested the change in the pull request.
4. Make sure the GitHub Actions build passes.

Never commit certificates, private keys, pairing codes or other secrets.

## Project layout

| Folder | Contents | Language |
|---|---|---|
| `windows/` | Media Foundation virtual camera | C++ |
| `receiver/` | WebRTC receiver and the Safari sender page | Go |

## License

ReCam is licensed under the [GNU General Public License v3.0](LICENSE). By contributing, you agree that your contributions are licensed under the same terms.
