// Package main serves HTTP traffic over Tailscale.
//
// Set the $VERSION environment variable and run `go generate`
// to update the embedded version.
//
//go:generate sh -c "printf 'package main\n\nconst Version = `%s`\n' $VERSION > version.go"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

var (
	hostname   = flag.String("hostname", "", "hostname to use")
	backend    = flag.String("backend", "", "target URL to proxy to")
	listenPort = flag.Int("listen-port", 443, "port to listen on")
	funnel     = flag.Bool("funnel", false, "enable funnel mode")
	mountPath  = flag.String("mount-path", "/", "path to mount proxy on")
	stateDir   = flag.String("state-dir", "/state", "directory to store state in")
	controlURL = flag.String("control-url", "", "control URL to use, leave empty for default")
	version    = flag.Bool("version", false, "print version and exit")
)

func main() {
	flag.Parse()

	if *version {
		rev := "unknown"
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				if s.Key == "vcs.revision" {
					rev = s.Value
					break
				}
			}
		}

		fmt.Printf("%s %s (%s)\n", os.Args[0], Version, rev)
		return
	}

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
	portStr := strconv.Itoa(*listenPort)

	if p := os.Getenv("TSNS_MOUNT_PATH"); p != "" {
		*mountPath = p
	}

	if d := os.Getenv("TSNS_STATE_DIR"); d != "" {
		*stateDir = d
	}

	proxyTarget, err := validateProxyTarget(*backend)
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
	defer s.Close()

	if *funnel {
		log.Printf("funneling traffic to %s", proxyTarget)
		if err := startFunnel(s, portStr, proxyTarget); err != nil {
			log.Fatalf("failed to start funnel: %v", err)
		}
	} else {
		log.Printf("proxying traffic to %s", proxyTarget)
		if err := startServer(context.Background(), s, portStr, proxyTarget.String()); err != nil {
			log.Fatalf("failed to start server: %v", err)
		}
	}

	// Wait forever.
	select {}
}

func startFunnel(s *tsnet.Server, portStr string, proxyTarget *url.URL) error {
	ln, err := s.ListenFunnel("tcp", ":"+portStr)
	if err != nil {
		log.Fatalf("ListenFunnel error: %v", err)
	}

	// Strip trailing slash from the mount path to make the
	// reverse proxy path work correctly.
	*mountPath = strings.TrimSuffix(*mountPath, "/")

	proxy := httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(proxyTarget)

			// Strip the mount path from the Out URL.
			r.Out.URL.Path = r.In.URL.Path[len(*mountPath):]
		},
	}

	return http.Serve(ln, &proxy)
}

func startServer(ctx context.Context, s *tsnet.Server, portStr, proxyTarget string) error {
	lc, err := s.LocalClient()
	if err != nil {
		return err
	}

	st, err := s.Up(ctx)
	if err != nil {
		return err
	}

	if len(st.CertDomains) == 0 {
		return fmt.Errorf("no cert domains, enable HTTPS")
	}

	domain := st.CertDomains[0]
	hp := ipn.HostPort(domain + ":" + portStr)

	srvConfig := &ipn.ServeConfig{
		TCP: map[uint16]*ipn.TCPPortHandler{
			uint16(*listenPort): {HTTPS: true},
		},
		Web: map[ipn.HostPort]*ipn.WebServerConfig{
			hp: {
				Handlers: map[string]*ipn.HTTPHandler{
					*mountPath: {Proxy: proxyTarget},
				},
			},
		},
	}

	// This kicks off the server.
	return lc.SetServeConfig(ctx, srvConfig)
}

func validateProxyTarget(source string) (*url.URL, error) {
	if !strings.Contains(source, "://") {
		source = "http://" + source
	}
	u, err := url.ParseRequestURI(source)
	if err != nil {
		return nil, fmt.Errorf("parsing url: %w", err)
	}
	switch u.Scheme {
	case "http", "https", "https+insecure":
		// ok
	default:
		return nil, fmt.Errorf("must be a URL starting with http://, https://, or https+insecure://")
	}

	return u, nil
}
