package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"

	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

var (
	hostname   = flag.String("hostname", "", "hostname to use")
	backend    = flag.String("backend", "", "target URL to proxy to")
	listenPort = flag.Int("listen-port", 443, "port to listen on")
	mountPath  = flag.String("mount-path", "/", "path to mount proxy on")
	stateDir   = flag.String("state-dir", "/state", "directory to store state in")
)

func main() {
	flag.Parse()

	if os.Getenv("TSNS_HOSTNAME") != "" {
		*hostname = os.Getenv("TSNS_HOSTNAME")
	}

	if os.Getenv("TSNS_BACKEND") != "" {
		*backend = os.Getenv("TSNS_BACKEND")
	}

	if os.Getenv("TSNS_LISTEN_PORT") != "" {
		p, err := strconv.Atoi(os.Getenv("TSNS_LISTEN_PORT"))
		if err != nil {
			log.Fatalf("invalid TSNS_LISTEN_PORT: %v", err)
		}
		*listenPort = p
	}

	if os.Getenv("TSNS_MOUNT_PATH") != "" {
		*mountPath = os.Getenv("TSNS_MOUNT_PATH")
	}

	if os.Getenv("TSNS_STATE_DIR") != "" {
		*stateDir = os.Getenv("TSNS_STATE_DIR")
	}

	if *hostname == "" {
		log.Fatal("hostname is required")
	}

	if *listenPort < 1 || *listenPort > 65535 {
		log.Fatal("invalid port")
	}
	portStr := strconv.Itoa(*listenPort)

	if *backend == "" {
		log.Fatal("backend is required")
	}

	proxyTarget, err := expandProxyTarget(*backend)
	if err != nil {
		log.Fatalf("invalid backend: %v", err)
	}

	err = os.MkdirAll(*stateDir, 0755)
	if err != nil {
		log.Fatalf("failed to create state directory: %v", err)
	}

	fi, err := os.Stat(*stateDir)
	if err != nil {
		log.Fatalf("failed to stat state directory: %v", err)
	}
	if fi.Mode().Perm()&0200 == 0 {
		log.Fatalf("state directory is not writable")
	}

	s := &tsnet.Server{
		Hostname: *hostname,
		Dir:      *stateDir,
	}
	defer s.Close()

	lc, err := s.LocalClient()
	if err != nil {
		log.Fatalf("failed to get local client: %v", err)
	}
	ctx := context.Background()
	st, err := s.Up(ctx)
	if err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
	if len(st.CertDomains) == 0 {
		log.Fatalf("no cert domains, enable HTTPS")
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
	err = lc.SetServeConfig(ctx, srvConfig)
	if err != nil {
		log.Fatalf("failed to set serve config: %v", err)
	}

	// Wait forever.
	select {}
}

func expandProxyTarget(source string) (string, error) {
	if !strings.Contains(source, "://") {
		source = "http://" + source
	}
	u, err := url.ParseRequestURI(source)
	if err != nil {
		return "", fmt.Errorf("parsing url: %w", err)
	}
	switch u.Scheme {
	case "http", "https", "https+insecure":
		// ok
	default:
		return "", fmt.Errorf("must be a URL starting with http://, https://, or https+insecure://")
	}

	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if port == 0 || err != nil {
		return "", fmt.Errorf("invalid port %q: %w", u.Port(), err)
	}

	host := u.Hostname()
	url := u.Scheme + "://" + host
	if u.Port() != "" {
		url += ":" + u.Port()
	}
	url += u.Path
	return url, nil
}
