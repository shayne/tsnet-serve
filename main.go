// Package main serves HTTP traffic over Tailscale.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/felixge/httpsnoop"
	"tailscale.com/tsnet"
)

var (
	hostname = flag.String("hostname", "tsnet-serve", "hostname to use")
	backend  = flag.String("backend", "8080", "target URL to proxy to")

	listenPort = flag.Int("listen-port", 443, "port to listen on")
	funnel     = flag.Bool("funnel", false, "enable funnel mode")

	allowedPaths      = flag.String("allowed-paths", "", "regex of paths to allow")
	deniedPaths       = flag.String("denied-paths", "", "regex of paths to deny")
	regexAllowedPaths *regexp.Regexp
	regexDeniedPaths  *regexp.Regexp

	stateDir   = flag.String("state-dir", "state", "directory to store state in")
	controlURL = flag.String("control-url", "", "control URL to use, leave empty for default")

	printVersion = flag.Bool("version", false, "print version and exit")

	version = "devel"
)

func init() {
	flag.Parse()

	if h := os.Getenv("TSNS_HOSTNAME"); h != "" {
		*hostname = h
	}
	if *hostname == "" {
		log.Fatal("hostname is required")
	}

	if b := os.Getenv("TSNS_BACKEND"); b != "" {
		*backend = b
	}
	if *backend == "" {
		log.Fatal("backend is required")
	}

	if fn := os.Getenv("TSNS_FUNNEL"); fn != "" {
		ok, err := strconv.ParseBool(fn)
		if err != nil {
			log.Fatalf("invalid TSNS_FUNNEL: %v", err)
		}
		*funnel = ok
	}

	if p := os.Getenv("TSNS_LISTEN_PORT"); p != "" {
		pr, err := strconv.Atoi(p)
		if err != nil {
			log.Fatalf("invalid TSNS_LISTEN_PORT: %v", err)
		}
		*listenPort = pr
	}
	if *listenPort < 1 || *listenPort > 65535 {
		log.Fatal("invalid port")
	}
	if *funnel && *listenPort != 443 && *listenPort != 8443 && *listenPort != 10000 {
		log.Fatal("funnel mode is only available on port 443, 8443, or 10000")
	}

	if p := os.Getenv("TSNS_ALLOWED_PATHS"); p != "" {
		*allowedPaths = p
	}
	if p := os.Getenv("TSNS_DENIED_PATHS"); p != "" {
		*deniedPaths = p
	}
	if *allowedPaths != "" {
		regexAllowedPaths = regexp.MustCompile(*allowedPaths)
	}
	if *deniedPaths != "" {
		regexDeniedPaths = regexp.MustCompile(*deniedPaths)
	}

	if d := os.Getenv("TSNS_STATE_DIR"); d != "" {
		*stateDir = d
	}
}

func main() {
	if *printVersion {
		fmt.Printf("%s %s\ntailscale %v\n", filepath.Base(os.Args[0]), version, tailscaleVersion())
		return
	}

	proxyTarget, err := expandProxyTarget(*backend)
	if err != nil {
		log.Fatalf("invalid backend: %v", err)
	}

	if err := os.MkdirAll(*stateDir, 0755); err != nil {
		log.Fatalf("failed to create state directory: %v", err)
	}

	fi, err := os.Stat(*stateDir)
	if err != nil {
		log.Fatalf("failed to stat state directory: %v", err)
	}
	if fi.Mode().Perm()&0200 == 0 {
		log.Fatalf("state directory is not writable")
	}

	if u := os.Getenv("TS_CONTROL_URL"); u != "" {
		*controlURL = u
	}

	s := &tsnet.Server{
		Hostname:   *hostname,
		Dir:        *stateDir,
		ControlURL: *controlURL,
	}
	defer func() {
		if err := s.Close(); err != nil {
			log.Fatalf("failed to close server: %v", err)
		}
	}()

	addr := ":" + strconv.Itoa(*listenPort)
	var ln net.Listener
	if *funnel {
		ln, err = s.ListenFunnel("tcp", addr)
	} else {
		ln, err = s.ListenTLS("tcp", addr)
	}
	if err != nil {
		log.Fatalf("failed to start listener: %v", err)
	}
	log.Printf("listening on %s", ln.Addr())

	rp := makeReverseProxy(proxyTarget)
	server := &http.Server{
		Handler: rp,
	}
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("failed to serve: %v", err)
	}

	ctx := context.Background()
	signal.NotifyContext(ctx, os.Interrupt)

	// Wait for an interrupt signal to gracefully shut down the server.
	<-ctx.Done()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("failed to shut down server: %v", err)
	}
}

func makeReverseProxy(proxyTarget *url.URL) http.Handler {
	hdl := makeProxyHandler(proxyTarget)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics := httpsnoop.CaptureMetrics(hdl, w, r)
		log.Printf("%s %s %s %d %s", r.RemoteAddr, r.Method, r.URL.Path, metrics.Code, metrics.Duration)
	})

}

func makeProxyHandler(proxyTarget *url.URL) http.Handler {
	var transport http.RoundTripper
	if proxyTarget.Scheme == "https+insecure" {
		proxyTarget.Scheme = "https"

		tsport := http.DefaultTransport.(*http.Transport).Clone()
		tsport.TLSClientConfig.InsecureSkipVerify = true
		transport = tsport
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetXForwarded()
			r.SetURL(proxyTarget)
		},
		Transport: transport,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if regexDeniedPaths != nil && regexDeniedPaths.MatchString(r.URL.Path) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		if regexAllowedPaths != nil && !regexAllowedPaths.MatchString(r.URL.Path) {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		rp.ServeHTTP(w, r)
	})
}

// expandProxyTarget returns a url from s, where s can be of the form:
//
// port number: "1234" -> "http://127.0.0.1:1234"
// host:port "example.com:1234" -> "http://example.com:1234"
// full URL "http://example.com:1234" -> "http://example.com:1234"
// insecure TLS "https+insecure://example.com:1234" -> "https+insecure://example.com:1234"
func expandProxyTarget(source string) (*url.URL, error) {
	if allNumeric(source) {
		source = "http://127.0.0.1:" + source
	} else if !strings.Contains(source, "://") {
		source = "http://" + source
	}

	u, err := url.ParseRequestURI(source)
	if err != nil {
		return nil, fmt.Errorf("error parsing url: %w", err)
	}

	switch u.Scheme {
	case "http", "https", "https+insecure":
		// ok
	default:
		return nil, fmt.Errorf("must be a URL starting with http://, https://, or https+insecure://")
	}

	return u, nil
}

func allNumeric(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

func tailscaleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	for _, m := range bi.Deps {
		if m.Path == "tailscale.com" {
			return m.Version
		}
	}

	return "unknown"
}
