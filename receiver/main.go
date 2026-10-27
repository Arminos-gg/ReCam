// Command recam-receiver receives the iPhone's camera from the ReCam Safari page over
// WebRTC and shows it on a preview page on this PC. Later steps feed the same stream
// into the virtual camera.
package main

import (
	"crypto/tls"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed web
var webFiles embed.FS

func main() {
	port := flag.Int("port", 8443, "HTTPS port the phone opens; WebRTC uses the same number over UDP")
	previewPort := flag.Int("preview-port", 8080, "port of the preview page, reachable from this PC only")
	dataDir := flag.String("data", "", "folder for the certificates and pairing code (default: the user config folder)")
	noBrowser := flag.Bool("no-browser", false, "do not open the preview page on start")
	ipFlag := flag.String("ip", "", "address of this PC to put in the phone's link, if the automatic choice is wrong")
	flag.Parse()

	dir := *dataDir
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			log.Fatal(err)
		}
		dir = filepath.Join(base, "ReCam")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Fatal(err)
	}

	ips := lanIPs()
	if *ipFlag != "" {
		ip := net.ParseIP(*ipFlag).To4()
		if ip == nil {
			log.Fatalf("-ip %q is not an IPv4 address", *ipFlag)
		}
		ips = append([]net.IP{ip}, ips...)
	}
	if len(ips) == 0 {
		log.Fatal("no network address found; connect this PC to the same Wi-Fi network as the phone")
	}
	crt, err := loadOrCreateCerts(dir, ips)
	if err != nil {
		log.Fatal(err)
	}
	token, err := loadOrCreateToken(dir)
	if err != nil {
		log.Fatal(err)
	}
	api, err := newWebRTCAPI(*port)
	if err != nil {
		log.Fatalf("WebRTC on UDP port %d: %v", *port, err)
	}
	h, err := newHub(api)
	if err != nil {
		log.Fatal(err)
	}

	web, _ := fs.Sub(webFiles, "web")
	host := preferredIP(ips)
	if *ipFlag != "" {
		host = ips[0]
	}
	phoneURL := fmt.Sprintf("https://%s:%d/?t=%s", host, *port, token)
	previewURL := fmt.Sprintf("http://127.0.0.1:%d/view.html", *previewPort)

	// the page the phone opens
	phone := http.NewServeMux()
	phone.Handle("/", noCache(http.FileServerFS(web)))
	phone.HandleFunc("/whip", h.handleWHIP(token))
	phone.HandleFunc("/whip/", h.handleWHIP(token))
	phone.HandleFunc("/recam-ca.crt", func(w http.ResponseWriter, r *http.Request) {
		// iOS offers to install a profile for this content type
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="ReCam-CA.crt"`)
		w.Write(crt.caPEM)
	})
	phoneSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", *port),
		Handler:           phone,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{crt.tls}},
		ReadHeaderTimeout: 10 * time.Second,
	}

	// the preview page on this PC; plain HTTP is a secure context on 127.0.0.1
	preview := http.NewServeMux()
	preview.Handle("/", noCache(http.FileServerFS(web)))
	preview.HandleFunc("/whep", h.handleWHEP)
	preview.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"phoneConnected": h.phoneConnected(),
			"phonePaused":    h.paused.Load(),
			"phoneURL":       phoneURL,
		})
	})
	preview.HandleFunc("/qr.png", func(w http.ResponseWriter, r *http.Request) {
		png, err := qrcode.Encode(phoneURL, qrcode.Medium, 320)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	})
	previewSrv := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", *previewPort),
		Handler:           preview,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() { log.Fatal(phoneSrv.ListenAndServeTLS("", "")) }()
	go func() { log.Fatal(previewSrv.ListenAndServe()) }()
	go logStats(h)

	if qr, err := qrcode.New(phoneURL, qrcode.Medium); err == nil {
		fmt.Println(qr.ToSmallString(false))
	}
	fmt.Printf("ReCam receiver is running.\n\n")
	fmt.Printf("  On the iPhone, scan the QR code or open: %s\n", phoneURL)
	fmt.Printf("  Preview on this PC:                      %s\n", previewURL)
	fmt.Printf("  Certificate for the phone (optional):    https://%s:%d/recam-ca.crt\n\n", host, *port)

	warnPublicNetworks()

	if !*noBrowser {
		openBrowser(previewURL)
	}
	select {}
}

const pausedBelowKbps = 200

func logStats(h *hub) {
	const every = 5 * time.Second
	var lastPackets, lastBytes, lastFrames, lastLost, lastKeys uint64
	for range time.Tick(every) {
		s := &h.stats
		p, b, f, l, k := s.packets.Load(), s.bytes.Load(), s.frames.Load(), s.lost.Load(), s.keyframes.Load()
		if p != lastPackets {
			secs := every.Seconds()
			fps, kbps := float64(f-lastFrames)/secs, float64(b-lastBytes)*8/1000/secs
			log.Printf("phone: %.1f fps, %.0f kbit/s, %d packets missing before retransmission, %d keyframes",
				fps, kbps, l-lastLost, k-lastKeys)

			// real 720p video needs well over 500 kbit/s; a trickle at full frame rate
			// means Safari is sending empty frames because iOS paused the camera
			paused := fps >= 1 && kbps < pausedBelowKbps
			if paused && !h.paused.Load() {
				log.Printf("phone: sending almost nothing, so its camera is paused. Is the screen locked or the ReCam page hidden?")
			}
			h.paused.Store(paused)
		}
		lastPackets, lastBytes, lastFrames, lastLost, lastKeys = p, b, f, l, k
	}
}

// lanIPs lists this PC's private IPv4 addresses, the ones a phone on the same
// Wi-Fi can reach, or any other IPv4 addresses if it has no private ones.
func lanIPs() []net.IP {
	var ips, others []net.IP
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if ip := n.IP.To4(); ip != nil && ip.IsPrivate() {
					ips = append(ips, ip)
				} else if ip != nil && ip.IsGlobalUnicast() {
					others = append(others, ip)
				}
			}
		}
	}
	if len(ips) == 0 {
		return others
	}
	return ips
}

// preferredIP picks the address of the interface that holds the default route,
// which is normally the Wi-Fi or Ethernet adapter rather than a VPN or VM adapter.
// Dialing UDP sends no packets.
func preferredIP(ips []net.IP) net.IP {
	if c, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		local := c.LocalAddr().(*net.UDPAddr).IP
		c.Close()
		for _, ip := range ips {
			if ip.Equal(local) {
				return ip
			}
		}
	}
	return ips[0]
}

// loadOrCreateToken returns the pairing code that the phone's URL carries. It is kept
// across restarts so a bookmark or Home Screen icon on the phone keeps working.
func loadOrCreateToken(dir string) (string, error) {
	path := filepath.Join(dir, "pairing-code")
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 16 {
		return strings.TrimSpace(string(b)), nil
	}
	token := randomHex(12)
	return token, os.WriteFile(path, []byte(token), 0o600)
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
