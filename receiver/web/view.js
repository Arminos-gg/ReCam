// ReCam preview: shows the stream the receiver gets from the phone, with live stats.
'use strict';

const $ = (id) => document.getElementById(id);
const video = $('video'), statsEl = $('stats'), waiting = $('waiting');

let pc = null;
let last = null;

async function connect() {
  if (pc) pc.close();
  const conn = new RTCPeerConnection({ iceServers: [] });
  pc = conn;
  const tr = conn.addTransceiver('video', { direction: 'recvonly' });
  // play frames as soon as they are decodable; the LAN needs little buffering
  if ('jitterBufferTarget' in tr.receiver) tr.receiver.jitterBufferTarget = 0;
  conn.addEventListener('track', (e) => { video.srcObject = new MediaStream([e.track]); });
  conn.addEventListener('connectionstatechange', () => {
    if (conn === pc && (conn.connectionState === 'failed' || conn.connectionState === 'closed')) {
      setTimeout(() => conn === pc && connect().catch(retry), 1000);
    }
  });

  await conn.setLocalDescription(await conn.createOffer());
  await new Promise((resolve) => {
    if (conn.iceGatheringState === 'complete') return resolve();
    const t = setTimeout(resolve, 1000);
    conn.addEventListener('icegatheringstatechange', () => {
      if (conn.iceGatheringState === 'complete') { clearTimeout(t); resolve(); }
    });
  });
  const res = await fetch('/whep', { method: 'POST', headers: { 'Content-Type': 'application/sdp' }, body: conn.localDescription.sdp });
  if (!res.ok) throw new Error(await res.text());
  await conn.setRemoteDescription({ type: 'answer', sdp: await res.text() });
}

function retry() { setTimeout(() => connect().catch(retry), 2000); }

async function poll() {
  try {
    const s = await (await fetch('/status')).json();
    $('url').textContent = s.phoneURL;
    waiting.hidden = s.phoneConnected;
  } catch {
    waiting.hidden = false;
    $('url').textContent = 'The receiver is not running.';
  }
  await showStats();
}

async function showStats() {
  if (!pc) return;
  const report = await pc.getStats();
  let s;
  report.forEach((r) => { if (r.type === 'inbound-rtp' && r.kind === 'video') s = r; });
  if (!s) return;
  const lines = [
    `${s.frameWidth || '?'}x${s.frameHeight || '?'} @ ${Math.round(s.framesPerSecond || 0)} fps`,
  ];
  if (last) {
    const secs = (s.timestamp - last.timestamp) / 1000;
    lines.push(`${Math.round(((s.bytesReceived - last.bytesReceived) * 8) / 1000 / secs)} kbit/s`);
    const buffered = s.jitterBufferEmittedCount - last.jitterBufferEmittedCount;
    if (buffered > 0) {
      lines.push(`jitter buffer ${Math.round(((s.jitterBufferDelay - last.jitterBufferDelay) / buffered) * 1000)} ms`);
    }
    const decoded = s.framesDecoded - last.framesDecoded;
    if (decoded > 0 && s.totalDecodeTime !== undefined) {
      lines.push(`decode ${(((s.totalDecodeTime - last.totalDecodeTime) / decoded) * 1000).toFixed(1)} ms/frame`);
    }
  }
  lines.push(`lost ${s.packetsLost || 0} · retransmit requests ${s.nackCount || 0} · keyframe requests ${s.pliCount || 0}`);
  if (s.framesDropped) lines.push(`dropped frames ${s.framesDropped}`);
  if (s.decoderImplementation) lines.push(`decoder ${s.decoderImplementation}`);
  last = s;
  statsEl.textContent = lines.join('\n');
  statsEl.hidden = !waiting.hidden;
}

connect().catch(retry);
poll();
setInterval(poll, 1000);
