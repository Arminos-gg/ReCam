// ReCam sender: captures the iPhone camera and sends it to the receiver on the PC
// over WebRTC (WHIP). Safari encodes the H.264 in hardware.
'use strict';

const BITRATE = Number(new URLSearchParams(location.search).get('kbps') || 4000) * 1000;
const WIDTH = 1280, HEIGHT = 720, FPS = 30;

const token = new URLSearchParams(location.search).get('t') || '';
const $ = (id) => document.getElementById(id);
const video = $('preview'), statusEl = $('status'), statsEl = $('stats');

let facing = 'environment';
let stream = null;      // camera stream
let pc = null;          // connection to the receiver
let resource = null;    // WHIP session URL, for a clean DELETE on stop
let running = false;    // the user pressed Start and not Stop
let wakeLock = null;
let awake = 'not yet';  // whether iOS agreed to keep the screen on, shown in the stats
let retryTimer = null;
let statsTimer = null;
let lastOut = null;

function setStatus(text, cls = '') {
  statusEl.textContent = text;
  statusEl.className = cls;
}

async function openCamera() {
  const s = await navigator.mediaDevices.getUserMedia({
    audio: false,
    video: {
      facingMode: { ideal: facing },
      width: { ideal: WIDTH },
      height: { ideal: HEIGHT },
      frameRate: { ideal: FPS, max: FPS },
    },
  });
  const track = s.getVideoTracks()[0];
  // tell the encoder to keep the frame rate up rather than the sharpness
  track.contentHint = 'motion';
  track.addEventListener('ended', () => running && recover('camera stopped'));
  track.addEventListener('mute', () => running && setStatus('Camera paused by iOS. It resumes when you come back to this page.', 'warn'));
  track.addEventListener('unmute', () => running && pc && setStatus('Live', 'ok'));
  if (stream) stream.getTracks().forEach((t) => t.stop());
  stream = s;
  video.srcObject = s;
  // the front camera is shown mirrored here only; the PC gets the unmirrored image
  video.style.transform = facing === 'user' ? 'scaleX(-1)' : '';
  return track;
}

// H.264 Constrained Baseline first: it has no B-frames, so no frame waits on a later one.
function preferH264(transceiver) {
  if (!transceiver.setCodecPreferences || !RTCRtpSender.getCapabilities) return;
  const codecs = RTCRtpSender.getCapabilities('video').codecs;
  const rank = (c) => {
    const f = c.sdpFmtpLine || '';
    if (c.mimeType !== 'video/H264') return c.mimeType === 'video/rtx' ? 9 : 99;
    if (!f.includes('packetization-mode=1')) return 5;
    if (f.includes('profile-level-id=42e0')) return 0;
    if (f.includes('profile-level-id=4200')) return 1;
    return 2;
  };
  const sorted = codecs.filter((c) => rank(c) < 99).sort((a, b) => rank(a) - rank(b));
  if (!sorted.some((c) => c.mimeType === 'video/H264')) {
    throw new Error('This browser cannot send H.264 video. Use Safari on the iPhone.');
  }
  try { transceiver.setCodecPreferences(sorted); } catch (e) { console.warn('codec preferences', e); }
}

function waitForIce(conn, ms) {
  if (conn.iceGatheringState === 'complete') return Promise.resolve();
  return new Promise((resolve) => {
    const done = () => { clearTimeout(timer); resolve(); };
    const timer = setTimeout(done, ms);
    conn.addEventListener('icegatheringstatechange', () => conn.iceGatheringState === 'complete' && done());
  });
}

async function connect() {
  closeConnection();
  let track = stream && stream.getVideoTracks()[0];
  if (!track || track.readyState === 'ended') track = await openCamera();

  const conn = new RTCPeerConnection({ iceServers: [], bundlePolicy: 'max-bundle' });
  pc = conn;
  const tr = conn.addTransceiver(track, {
    direction: 'sendonly',
    streams: [stream],
    sendEncodings: [{ maxBitrate: BITRATE, maxFramerate: FPS }],
  });
  try { preferH264(tr); } catch (e) { running = false; throw e; }
  try {
    const p = tr.sender.getParameters();
    p.degradationPreference = 'maintain-framerate';
    if (p.encodings && p.encodings[0]) Object.assign(p.encodings[0], { maxBitrate: BITRATE, maxFramerate: FPS });
    await tr.sender.setParameters(p);
  } catch (e) { console.warn('sender parameters', e); }

  conn.addEventListener('connectionstatechange', () => {
    if (conn !== pc) return;
    switch (conn.connectionState) {
      case 'connected': setStatus('Live', 'ok'); break;
      case 'disconnected': setStatus('Connection interrupted, waiting…', 'warn'); break;
      case 'failed': recover('connection failed'); break;
    }
  });

  await conn.setLocalDescription(await conn.createOffer());
  // the receiver answers with all its candidates, so gather ours first instead of trickling
  await waitForIce(conn, 2000);
  const res = await fetch('/whip?t=' + encodeURIComponent(token), {
    method: 'POST',
    headers: { 'Content-Type': 'application/sdp' },
    body: conn.localDescription.sdp,
  });
  if (res.status === 401) {
    running = false;
    throw new Error('This link has an old pairing code. Scan the QR code on the PC again.');
  }
  if (!res.ok) throw new Error('Receiver said: ' + (await res.text()));
  resource = res.headers.get('Location');
  await conn.setRemoteDescription({ type: 'answer', sdp: await res.text() });
}

function closeConnection() {
  if (pc) { pc.close(); pc = null; }
  if (resource) {
    fetch(resource + '?t=' + encodeURIComponent(token), { method: 'DELETE', keepalive: true }).catch(() => {});
    resource = null;
  }
}

// recover reconnects after a failure, retrying every two seconds while the page is visible.
function recover(reason) {
  if (!running || retryTimer) return;
  setStatus('Reconnecting (' + reason + ')…', 'warn');
  retryTimer = setTimeout(async () => {
    retryTimer = null;
    if (!running || document.visibilityState !== 'visible') return;
    try { await connect(); } catch (e) {
      if (!running) return setStatus(e.message, 'bad');
      recover(e.message);
    }
  }, 2000);
}

async function start() {
  running = true;
  document.body.classList.add('live');
  $('start').hidden = true;
  $('stop').hidden = false;
  setStatus('Connecting…');
  keepAwake();
  try {
    await connect();
    keepAwake(); // the camera prompt can cost the lock taken above
  } catch (e) {
    if (e.name === 'NotAllowedError') {
      stop();
      return setStatus('Camera access was refused. Allow it in Safari\'s website settings (aA in the address bar), then press Start.', 'bad');
    }
    if (!running) { stop(); return setStatus(e.message, 'bad'); }
    recover(e.message);
  }
  statsTimer = statsTimer || setInterval(showStats, 1000);
}

function stop() {
  running = false;
  clearTimeout(retryTimer); retryTimer = null;
  clearInterval(statsTimer); statsTimer = null;
  closeConnection();
  if (stream) { stream.getTracks().forEach((t) => t.stop()); stream = null; }
  video.srcObject = null;
  if (wakeLock) { wakeLock.release().catch(() => {}); wakeLock = null; }
  document.body.classList.remove('live');
  $('start').hidden = false;
  $('stop').hidden = true;
  statsEl.hidden = true;
  setStatus('Stopped.');
}

async function flip() {
  facing = facing === 'environment' ? 'user' : 'environment';
  $('flip').textContent = facing === 'environment' ? 'Front camera' : 'Back camera';
  if (!running) return;
  try {
    const track = await openCamera();
    const sender = pc && pc.getSenders()[0];
    if (sender) await sender.replaceTrack(track);
  } catch (e) { recover(e.message); }
}

// Without this, iOS auto-locks the phone after its Auto-Lock delay (30 s at the
// shortest) and the camera stops. iOS also drops the lock when the page is hidden,
// so it is taken again whenever it is released while streaming.
async function keepAwake() {
  if (!running || document.visibilityState !== 'visible' || (wakeLock && !wakeLock.released)) return;
  if (!('wakeLock' in navigator)) { awake = 'not supported'; return showAwakeWarning(); }
  try {
    const lock = await navigator.wakeLock.request('screen');
    wakeLock = lock;
    awake = 'yes';
    lock.addEventListener('release', () => {
      awake = 'no';
      if (running && document.visibilityState === 'visible') setTimeout(keepAwake, 500);
    });
  } catch (e) {
    awake = 'refused (' + e.name + ')';
    console.warn('wake lock', e);
    showAwakeWarning();
  }
}

function showAwakeWarning() {
  if (running) setStatus('iOS would not keep the screen on, so the camera stops when the phone locks. Set Settings › Display & Brightness › Auto-Lock to Never while streaming, or tap the screen to try again.', 'warn');
}

// a tap counts as a user gesture, which some iOS versions want before granting the lock
document.addEventListener('click', () => keepAwake());

// iOS stops the camera while the page is hidden; get it back when the page returns.
document.addEventListener('visibilitychange', async () => {
  if (!running || document.visibilityState !== 'visible') return;
  keepAwake();
  const track = stream && stream.getVideoTracks()[0];
  try {
    if (!track || track.readyState === 'ended') {
      const fresh = await openCamera();
      const sender = pc && pc.connectionState === 'connected' && pc.getSenders()[0];
      if (sender) await sender.replaceTrack(fresh);
      else await connect();
    } else if (!pc || pc.connectionState === 'failed' || pc.connectionState === 'closed') {
      await connect();
    }
  } catch (e) { recover(e.message); }
});

window.addEventListener('pagehide', closeConnection);

async function showStats() {
  if (!pc) return;
  const report = await pc.getStats();
  let out, remote, codec;
  report.forEach((s) => {
    if (s.type === 'outbound-rtp' && s.kind === 'video') out = s;
    if (s.type === 'remote-inbound-rtp' && s.kind === 'video') remote = s;
  });
  if (!out) return;
  codec = out.codecId && report.get(out.codecId);
  let kbps = 0;
  if (lastOut) kbps = ((out.bytesSent - lastOut.bytesSent) * 8) / (out.timestamp - lastOut.timestamp);
  lastOut = out;
  const lines = [
    `${out.frameWidth || '?'}x${out.frameHeight || '?'} @ ${Math.round(out.framesPerSecond || 0)} fps`,
    `${Math.round(kbps)} kbit/s of ${BITRATE / 1000}`,
    codec ? `${codec.mimeType.replace('video/', '')} ${(codec.sdpFmtpLine || '').match(/profile-level-id=\w+/)?.[0] || ''}` : '',
    remote && remote.roundTripTime !== undefined ? `round trip ${Math.round(remote.roundTripTime * 1000)} ms` : '',
    remote && remote.fractionLost !== undefined ? `loss ${(remote.fractionLost * 100).toFixed(1)}%` : '',
    out.qualityLimitationReason && out.qualityLimitationReason !== 'none' ? `limited by ${out.qualityLimitationReason}` : '',
    `screen kept on: ${awake}`,
  ];
  statsEl.textContent = lines.filter(Boolean).join('\n');
  statsEl.hidden = false;
}

$('start').addEventListener('click', start);
$('stop').addEventListener('click', stop);
$('flip').addEventListener('click', flip);

if (!window.isSecureContext || !navigator.mediaDevices) {
  setStatus('This page needs HTTPS to use the camera. Open the address shown on the PC, starting with https://', 'bad');
  $('start').disabled = true;
} else if (!token) {
  setStatus('Pairing code missing. Scan the QR code on the PC.', 'bad');
  $('start').disabled = true;
}
