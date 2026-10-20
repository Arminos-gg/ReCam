# Security Policy

ReCam streams camera video over your local network and runs a local HTTPS server with its own certificate, so security problems matter even at this early stage.

## Supported versions

ReCam has no released versions yet. Security fixes are made on the `main` branch.

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

Report them privately through [GitHub's private vulnerability reporting](https://github.com/Arminos-gg/ReCam/security/advisories/new). Include:

- what the problem is and what an attacker could do with it,
- steps to reproduce it,
- the ReCam commit or version, and your Windows and iOS versions.

You should get a reply within a week. Once the problem is fixed, the advisory will be published and you will be credited unless you prefer not to be.

## Things that are especially relevant

- Anyone on the same network being able to view or inject video.
- Weaknesses in pairing, the local certificate authority or HTTPS setup.
- The receiver listening on more network interfaces or ports than it needs.
- Video frames or keys being written to disk unexpectedly.
