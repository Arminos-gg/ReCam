package main

import (
	"bytes"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/h264reader"
)

// TestPhoneToPreview sends a real H.264 clip the way the Safari page does (WHIP) and
// checks that a preview client (WHEP) receives the frames, including after the phone
// reconnects.
func TestPhoneToPreview(t *testing.T) {
	api, err := newWebRTCAPI(18443)
	if err != nil {
		t.Fatal(err)
	}
	h, err := newHub(api)
	if err != nil {
		t.Fatal(err)
	}
	const token = "test-token-123456"
	mux := http.NewServeMux()
	mux.HandleFunc("/whip", h.handleWHIP(token))
	mux.HandleFunc("/whip/", h.handleWHIP(token))
	mux.HandleFunc("/whep", h.handleWHEP)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// a wrong pairing code is refused
	res, err := http.Post(srv.URL+"/whip?t=nope", "application/sdp", strings.NewReader("v=0"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: got %d, want 401", res.StatusCode)
	}

	var frames, keyframes atomic.Int64
	viewer := connectViewer(t, srv.URL+"/whep", func(p *rtp.Packet) {
		if p.Marker {
			frames.Add(1)
		}
		if isKeyframeStart(p.Payload) {
			keyframes.Add(1)
		}
	})
	defer viewer.Close()

	for round := 1; round <= 2; round++ {
		before := frames.Load()
		phone, done := connectPhone(t, srv.URL+"/whip?t="+token)
		waitFor(t, 5*time.Second, func() bool { return h.phoneConnected() })
		<-done
		phone.Close()

		got := frames.Load() - before
		// the clip has 60 frames; allow for the first few arriving before the preview's
		// connection settles
		if got < 45 {
			t.Fatalf("round %d: preview received %d frames, want at least 45", round, got)
		}
		t.Logf("round %d: preview received %d frames", round, got)
	}
	if keyframes.Load() < 2 {
		t.Fatalf("preview saw %d keyframes, want at least 2", keyframes.Load())
	}
	if h.stats.frames.Load() == 0 {
		t.Fatal("receiver counted no frames")
	}
}

func connectViewer(t *testing.T, url string, onPacket func(*rtp.Packet)) *webrtc.PeerConnection {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			p, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			onPacket(p)
		}
	})
	exchange(t, pc, url)
	waitFor(t, 5*time.Second, func() bool { return pc.ConnectionState() == webrtc.PeerConnectionStateConnected })
	return pc
}

// connectPhone streams the test clip at 30 fps and closes done when it has been sent.
func connectPhone(t *testing.T, url string) (*webrtc.PeerConnection, <-chan struct{}) {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: h264FMTP},
		"video", "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pc.AddTrack(track); err != nil {
		t.Fatal(err)
	}
	exchange(t, pc, url)

	frames := readAccessUnits(t, "testdata/testsrc-320x180-30fps.h264")
	done := make(chan struct{})
	go func() {
		defer close(done)
		connected := make(chan struct{})
		pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
			if s == webrtc.PeerConnectionStateConnected {
				select {
				case <-connected:
				default:
					close(connected)
				}
			}
		})
		if pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
			select {
			case <-connected:
			case <-time.After(5 * time.Second):
				return
			}
		}
		tick := time.NewTicker(time.Second / 30)
		defer tick.Stop()
		for _, f := range frames {
			<-tick.C
			if err := track.WriteSample(media.Sample{Data: f, Duration: time.Second / 30}); err != nil {
				return
			}
		}
		time.Sleep(300 * time.Millisecond) // let the last packets arrive
	}()
	return pc, done
}

func exchange(t *testing.T, pc *webrtc.PeerConnection, url string) {
	t.Helper()
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	res, err := http.Post(url, "application/sdp", strings.NewReader(pc.LocalDescription().SDP))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("%s: %d %s", url, res.StatusCode, body)
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(body)}); err != nil {
		t.Fatal(err)
	}
}

// readAccessUnits splits an Annex-B file into one sample per frame, keeping SPS and
// PPS with the frame that follows them.
func readAccessUnits(t *testing.T, path string) [][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := h264reader.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var units [][]byte
	var cur bytes.Buffer
	for {
		nal, err := r.NextNAL()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		cur.Write([]byte{0, 0, 0, 1})
		cur.Write(nal.Data)
		if nal.UnitType == h264reader.NalUnitTypeCodedSliceNonIdr || nal.UnitType == h264reader.NalUnitTypeCodedSliceIdr {
			units = append(units, bytes.Clone(cur.Bytes()))
			cur.Reset()
		}
	}
	if len(units) == 0 {
		t.Fatal("no frames in test clip")
	}
	return units
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestIsKeyframeStart(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    bool
	}{
		{"single IDR", []byte{0x65, 0x88}, true},
		{"single non-IDR", []byte{0x41, 0x9a}, false},
		{"single SPS", []byte{0x67, 0x42}, false},
		{"STAP-A with SPS, PPS, IDR", []byte{0x78, 0, 2, 0x67, 0x42, 0, 2, 0x68, 0xce, 0, 2, 0x65, 0x88}, true},
		{"STAP-A with SPS, PPS only", []byte{0x78, 0, 2, 0x67, 0x42, 0, 2, 0x68, 0xce}, false},
		{"FU-A IDR start", []byte{0x7c, 0x85, 0x88}, true},
		{"FU-A IDR middle", []byte{0x7c, 0x05, 0x88}, false},
		{"FU-A non-IDR start", []byte{0x7c, 0x81, 0x9a}, false},
		{"too short", []byte{0x65}, false},
	}
	for _, c := range cases {
		if got := isKeyframeStart(c.payload); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCertificates(t *testing.T) {
	dir := t.TempDir()
	ip1 := net.IPv4(192, 168, 1, 20).To4()
	c1, err := loadOrCreateCerts(dir, []net.IP{ip1})
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(c1.caPEM) {
		t.Fatal("CA PEM did not parse")
	}
	leaf, err := x509.ParseCertificate(c1.tls.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: "192.168.1.20", Roots: pool}); err != nil {
		t.Fatalf("server certificate does not verify for the LAN address: %v", err)
	}
	if d := leaf.NotAfter.Sub(leaf.NotBefore); d > 398*24*time.Hour {
		t.Fatalf("server certificate valid for %v, iOS rejects more than 398 days", d)
	}

	// same address: reused; new address: re-issued under the same CA
	c2, err := loadOrCreateCerts(dir, []net.IP{ip1})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c1.tls.Certificate[0], c2.tls.Certificate[0]) {
		t.Fatal("certificate was re-issued although nothing changed")
	}
	ip2 := net.IPv4(10, 0, 0, 5).To4()
	c3, err := loadOrCreateCerts(dir, []net.IP{ip2})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(c1.tls.Certificate[0], c3.tls.Certificate[0]) {
		t.Fatal("certificate was not re-issued for a new address")
	}
	if !bytes.Equal(c1.caPEM, c3.caPEM) {
		t.Fatal("CA changed; the phone would have to trust it again")
	}
}
