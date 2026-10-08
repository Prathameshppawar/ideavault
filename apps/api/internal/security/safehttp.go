package security

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrBlockedAddress is returned when a URL resolves to a non-public address.
var ErrBlockedAddress = errors.New("destination address is not allowed (private, loopback or link-local)")

// SafeFetcher fetches public http(s) URLs with SSRF protections:
//   - only http/https, ports 80/443 (configurable),
//   - every resolved IP (including after redirects) must be a public unicast address,
//   - the check happens at dial time (defeats DNS rebinding),
//   - bounded redirects, timeout and response size.
type SafeFetcher struct {
	client       *http.Client
	MaxBytes     int64
	UserAgent    string
	AllowedPorts map[string]bool
	// AllowPrivate disables address checks (tests only).
	AllowPrivate bool
}

// NewSafeFetcher returns a fetcher with sane defaults.
func NewSafeFetcher(maxBytes int64, timeout time.Duration) *SafeFetcher {
	f := &SafeFetcher{
		MaxBytes:     maxBytes,
		UserAgent:    "Mozilla/5.0 (compatible; IdeaVault/1.0; +https://github.com/Prathameshppawar/ideavault)",
		AllowedPorts: map[string]bool{"80": true, "443": true},
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: f.control}
	transport := &http.Transport{
		Proxy:                 nil, // never route through env proxies
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	f.client = &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return f.checkURL(req.URL)
		},
	}
	return f
}

// control runs after DNS resolution, right before connecting: the authoritative SSRF check.
func (f *SafeFetcher) control(network, address string, _ syscall.RawConn) error {
	if f.AllowPrivate {
		return nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if !f.AllowedPorts[port] {
		return fmt.Errorf("port %s is not allowed", port)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return ErrBlockedAddress
	}
	if !IsPublicAddr(ip) {
		return ErrBlockedAddress
	}
	return nil
}

func (f *SafeFetcher) checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q is not allowed", u.Scheme)
	}
	if u.User != nil {
		return errors.New("URLs with embedded credentials are not allowed")
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("URL has no host")
	}
	if f.AllowPrivate {
		return nil
	}
	lh := strings.ToLower(host)
	if lh == "localhost" || strings.HasSuffix(lh, ".localhost") || strings.HasSuffix(lh, ".local") || strings.HasSuffix(lh, ".internal") {
		return ErrBlockedAddress
	}
	if ip, err := netip.ParseAddr(host); err == nil && !IsPublicAddr(ip) {
		return ErrBlockedAddress
	}
	return nil
}

// Fetch downloads a public URL. It returns the body, content type and final URL after redirects.
func (f *SafeFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, string, string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, "", "", fmt.Errorf("invalid URL: %w", err)
	}
	if err := f.checkURL(u); err != nil {
		return nil, "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json,text/markdown,text/plain;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, "", "", fmt.Errorf("the page requires sign-in or blocks automated access (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", "", errors.New("page not found (HTTP 404) — the link may be private, deleted or mistyped")
	}
	if resp.StatusCode >= 400 {
		return nil, "", "", fmt.Errorf("fetch failed: HTTP %d", resp.StatusCode)
	}
	lr := io.LimitReader(resp.Body, f.MaxBytes+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, "", "", err
	}
	if int64(len(body)) > f.MaxBytes {
		return nil, "", "", fmt.Errorf("response exceeds %d bytes", f.MaxBytes)
	}
	return body, resp.Header.Get("Content-Type"), resp.Request.URL.String(), nil
}

// IsPublicAddr reports whether ip is a globally routable unicast address.
func IsPublicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24",
		"203.0.113.0/24", "240.0.0.0/4", "255.255.255.255/32", "169.254.0.0/16",
		"64:ff9b::/96", "100::/64", "2001:db8::/32", "fc00::/7", "fe80::/10", "2002::/16",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()
