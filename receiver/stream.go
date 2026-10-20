package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// h264FMTP is what the phone is asked to send and what the preview advertises.
// Constrained Baseline has no B-frames, so no frame ever waits for a later one.
const h264FMTP = "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"

func newWebRTCAPI(udpPort int) (*webrtc.API, error) {
	m := &webrtc.MediaEngine{}
	feedback := []webrtc.RTCPFeedback{
		{Type: "goog-remb"}, {Type: "ccm", Parameter: "fir"}, {Type: "nack"}, {Type: "nack", Parameter: "pli"},
	}
	// H.264 only: iPhones encode it in hardware, and the decoder in step 3 expects it.
	profiles := []struct {
		pt   webrtc.PayloadType
		fmtp string
	}{
		{102, h264FMTP},
		{104, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f"},
		{106, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=4d001f"},
		{108, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=640c1f"},
	}
	for _, p := range profiles {
		err := m.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: p.fmtp, RTCPFeedback: feedback,
			},
			PayloadType: p.pt,
		}, webrtc.RTPCodecTypeVideo)
		if err != nil {
			return nil, err
		}
	}

	// NACK (retransmission of lost packets), RTCP reports and transport-wide congestion feedback
	reg := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, reg); err != nil {
		return nil, err
	}

	// Everything goes through one UDP port, so a single firewall rule covers it. A large
	// socket buffer keeps the burst of packets in a keyframe from overflowing it.
	mux, err := ice.NewMultiUDPMuxFromPort(udpPort,
		ice.UDPMuxFromPortWithNetworks(ice.NetworkTypeUDP4),
		ice.UDPMuxFromPortWithReadBufferSize(4<<20),
		ice.UDPMuxFromPortWithLoopback(),
	)
	if err != nil {
		return nil, err
	}
	s := webrtc.SettingEngine{}
	s.SetICEUDPMux(mux)
	s.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	s.SetIncludeLoopbackCandidate(true)

	return webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(reg), webrtc.WithSettingEngine(s)), nil
}

// hub holds the phone's connection (at most one; a new one replaces it) and fans its
// video out to preview pages without decoding it.
type hub struct {
	api   *webrtc.API
	local *webrtc.TrackLocalStaticRTP

	mu        sync.Mutex
	phone     *webrtc.PeerConnection
	phoneID   string
	phoneSSRC uint32

	out   rewriter
	stats streamStats
}

func newHub(api *webrtc.API) (*hub, error) {
	local, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: h264FMTP},
		"video", "recam",
	)
	if err != nil {
		return nil, err
	}
	return &hub{api: api, local: local}, nil
}

// handleWHIP accepts the phone's offer (WHIP, RFC 9725) and answers once ICE
// gathering is done, so no candidates need to be trickled.
func (h *hub) handleWHIP(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, token) {
			http.Error(w, "wrong pairing code, scan the QR code on the PC again", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPost:
			h.acceptPhone(w, r)
		case http.MethodDelete:
			h.dropPhone(strings.TrimPrefix(r.URL.Path, "/whip/"))
			w.WriteHeader(http.StatusOK)
		default:
			w.Header().Set("Allow", "POST, DELETE")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func (h *hub) acceptPhone(w http.ResponseWriter, r *http.Request) {
	offer, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pc, err := h.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		pc.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id := randomHex(8)
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		log.Printf("phone %s: receiving %s (%s)", id, track.Codec().MimeType, track.Codec().SDPFmtpLine)
		h.mu.Lock()
		if h.phoneID == id {
			h.phoneSSRC = uint32(track.SSRC())
		}
		h.mu.Unlock()
		h.forward(id, track)
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("phone %s: %s", id, s)
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			h.dropPhone(id)
		}
	})

	answer, err := answerAfterGathering(pc, string(offer))
	if err != nil {
		pc.Close()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// a reconnecting phone replaces its previous session
	h.mu.Lock()
	old := h.phone
	h.phone, h.phoneID, h.phoneSSRC = pc, id, 0
	h.mu.Unlock()
	if old != nil {
		old.Close()
	}
	h.stats.reset()

	w.Header().Set("Content-Type", "application/sdp")
	w.Header().Set("Location", "/whip/"+id)
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, answer)
}

func (h *hub) dropPhone(id string) {
	h.mu.Lock()
	pc := h.phone
	if pc == nil || h.phoneID != id {
		h.mu.Unlock()
		return
	}
	h.phone, h.phoneID, h.phoneSSRC = nil, "", 0
	h.mu.Unlock()
	pc.Close()
}

// forward copies the phone's RTP packets to the preview track as they arrive.
// Nothing is buffered here; the preview browser runs its own jitter buffer.
func (h *hub) forward(id string, track *webrtc.TrackRemote) {
	buf := make([]byte, 1500)
	var pkt rtp.Packet
	first := true
	for {
		n, _, err := track.Read(buf)
		if err != nil {
			log.Printf("phone %s: track ended: %v", id, err)
			return
		}
		if err := pkt.Unmarshal(buf[:n]); err != nil {
			continue
		}
		h.stats.add(&pkt, n)

		// a replaced session's goroutine may still deliver a few packets; drop them
		h.mu.Lock()
		current := h.phoneID == id
		if current {
			h.out.rewrite(&pkt, first)
		}
		h.mu.Unlock()
		if !current {
			return
		}
		first = false
		if err := h.local.WriteRTP(&pkt); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			log.Printf("preview: %v", err)
		}
	}
}

// rewriter keeps the preview's sequence numbers and timestamps continuous when the
// phone reconnects. A new session starts at random values, and a jump backwards would
// look like replayed packets to the preview's SRTP and be dropped.
type rewriter struct {
	started         bool
	seqOffset       uint16
	tsOffset        uint32
	lastSeq, lastTS uint32 // last values sent, as uint32 so zero is not ambiguous
}

func (r *rewriter) rewrite(p *rtp.Packet, newSession bool) {
	if newSession && r.started {
		r.seqOffset = uint16(r.lastSeq) + 1 - p.SequenceNumber
		r.tsOffset = r.lastTS + 3000 - p.Timestamp // one frame at 30 fps on the 90 kHz clock
	}
	p.SequenceNumber += r.seqOffset
	p.Timestamp += r.tsOffset
	r.started = true
	if int16(p.SequenceNumber-uint16(r.lastSeq)) > 0 || newSession {
		r.lastSeq, r.lastTS = uint32(p.SequenceNumber), p.Timestamp
	}
}

// requestKeyframe asks the phone for a new keyframe, so a preview that joins
// mid-stream does not wait for the next scheduled one.
func (h *hub) requestKeyframe() {
	h.mu.Lock()
	pc, ssrc := h.phone, h.phoneSSRC
	h.mu.Unlock()
	if pc == nil || ssrc == 0 {
		return
	}
	_ = pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}})
}

func (h *hub) phoneConnected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.phone != nil && h.phone.ConnectionState() == webrtc.PeerConnectionStateConnected
}

// handleWHEP serves the preview page's offer (WHEP) with the forwarded track.
func (h *hub) handleWHEP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	offer, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pc, err := h.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sender, err := pc.AddTrack(h.local)
	if err != nil {
		pc.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// read RTCP so NACKs are answered, and pass the preview's keyframe requests on
	go func() {
		for {
			pkts, _, err := sender.ReadRTCP()
			if err != nil {
				return
			}
			for _, p := range pkts {
				switch p.(type) {
				case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
					h.requestKeyframe()
				}
			}
		}
	}()
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		switch s {
		case webrtc.PeerConnectionStateConnected:
			h.requestKeyframe()
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateDisconnected:
			pc.Close()
		}
	})

	answer, err := answerAfterGathering(pc, string(offer))
	if err != nil {
		pc.Close()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/sdp")
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, answer)
}

func answerAfterGathering(pc *webrtc.PeerConnection, offer string) (string, error) {
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return "", err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return "", err
	}
	select {
	case <-gathered:
	case <-time.After(3 * time.Second):
		return "", errors.New("ICE gathering timed out")
	}
	return pc.LocalDescription().SDP, nil
}

func authorized(r *http.Request, token string) bool {
	if r.URL.Query().Get("t") == token {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+token
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// streamStats counts what arrives from the phone, for the log and the preview page.
type streamStats struct {
	packets, bytes, frames, keyframes, lost atomic.Uint64

	mu      sync.Mutex
	lastSeq uint16
	started bool
}

func (s *streamStats) reset() {
	s.mu.Lock()
	s.started = false
	s.mu.Unlock()
}

func (s *streamStats) add(pkt *rtp.Packet, size int) {
	s.packets.Add(1)
	s.bytes.Add(uint64(size))
	if pkt.Marker {
		s.frames.Add(1)
	}
	if isKeyframeStart(pkt.Payload) {
		s.keyframes.Add(1)
	}

	// a gap in sequence numbers is loss before NACK recovery; retransmitted packets
	// arrive with old numbers and are ignored here
	s.mu.Lock()
	if s.started {
		if gap := pkt.SequenceNumber - s.lastSeq; gap > 1 && gap < 1000 {
			s.lost.Add(uint64(gap - 1))
		}
		if int16(pkt.SequenceNumber-s.lastSeq) > 0 {
			s.lastSeq = pkt.SequenceNumber
		}
	} else {
		s.lastSeq, s.started = pkt.SequenceNumber, true
	}
	s.mu.Unlock()
}

// isKeyframeStart reports whether an H.264 RTP payload begins an IDR frame
// (RFC 6184: single NAL unit, inside a STAP-A, or the first FU-A fragment).
func isKeyframeStart(p []byte) bool {
	if len(p) < 2 {
		return false
	}
	switch nal := p[0] & 0x1f; nal {
	case 5:
		return true
	case 24: // STAP-A: walk the aggregated NAL units
		for i := 1; i+2 < len(p); {
			size := int(p[i])<<8 | int(p[i+1])
			if p[i+2]&0x1f == 5 {
				return true
			}
			i += 2 + size
		}
	case 28: // FU-A
		return p[1]&0x80 != 0 && p[1]&0x1f == 5
	}
	return false
}
