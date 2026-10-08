package security

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testSealer(t *testing.T) *Sealer {
	t.Helper()
	s, err := NewSealer([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSealerRoundTrip(t *testing.T) {
	s := testSealer(t)
	secret := []byte("sk-proj-very-secret-key-123456")
	ct, nonce, ver, err := s.Seal(secret, "user:model_provider:openai:api_key")
	if err != nil {
		t.Fatal(err)
	}
	if ver != 1 || len(nonce) != 12 {
		t.Errorf("version=%d nonce=%d bytes", ver, len(nonce))
	}
	if bytes.Contains(ct, secret) {
		t.Fatal("ciphertext contains the plaintext")
	}
	pt, err := s.Open(ct, nonce, "user:model_provider:openai:api_key")
	if err != nil || string(pt) != string(secret) {
		t.Fatalf("Open = %q, %v", pt, err)
	}
	// Two seals of the same secret differ (random nonce).
	ct2, nonce2, _, _ := s.Seal(secret, "user:model_provider:openai:api_key")
	if bytes.Equal(ct, ct2) || bytes.Equal(nonce, nonce2) {
		t.Error("sealing must be randomized")
	}
}

func TestSealerRejectsTamperingAndWrongContext(t *testing.T) {
	s := testSealer(t)
	ct, nonce, _, err := s.Seal([]byte("gsk_secret"), "aad-1")
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), ct...)
	tampered[0] ^= 0xff
	if _, err := s.Open(tampered, nonce, "aad-1"); err == nil {
		t.Error("tampered ciphertext must not decrypt")
	}
	if _, err := s.Open(ct, nonce, "aad-2"); err == nil {
		t.Error("wrong additional data (another owner) must not decrypt")
	}
	badNonce := append([]byte(nil), nonce...)
	badNonce[0] ^= 1
	if _, err := s.Open(ct, badNonce, "aad-1"); err == nil {
		t.Error("wrong nonce must not decrypt")
	}
	other, _ := NewSealer([]byte("ffffffffffffffffffffffffffffffff"))
	if _, err := other.Open(ct, nonce, "aad-1"); err == nil || !strings.Contains(err.Error(), "MASTER_KEY") {
		t.Errorf("wrong key error should mention MASTER_KEY, got %v", err)
	}
	for _, k := range [][]byte{nil, []byte("short"), make([]byte, 31), make([]byte, 33)} {
		if _, err := NewSealer(k); err == nil {
			t.Errorf("NewSealer accepted a %d-byte key", len(k))
		}
	}
}

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=2,p=2$") {
		t.Errorf("unexpected encoding %q", h)
	}
	if !VerifyPassword("correct horse battery", h) {
		t.Error("correct password rejected")
	}
	for _, wrong := range []string{"correct horse batterY", "", "correct horse battery "} {
		if VerifyPassword(wrong, h) {
			t.Errorf("wrong password %q accepted", wrong)
		}
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Error("hashes must be salted")
	}
	for _, bad := range []string{"", "plain", "$argon2id$v=19$m=65536,t=2,p=2$!!$!!", "$bcrypt$x$y$z$w", strings.Replace(h, "argon2id", "argon2i", 1)} {
		if VerifyPassword("correct horse battery", bad) {
			t.Errorf("malformed hash %q accepted", bad)
		}
	}
	if _, err := HashPassword("short"); err == nil {
		t.Error("passwords under 8 characters must be rejected")
	}
	if _, err := HashPassword(strings.Repeat("x", 1025)); err == nil {
		t.Error("absurdly long passwords must be rejected")
	}
}

func TestTokens(t *testing.T) {
	tok, hash, err := NewToken("ivs_")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "ivs_") || len(tok) < 40 {
		t.Errorf("token %q", tok)
	}
	if !bytes.Equal(hash, HashToken(tok)) || len(hash) != 32 {
		t.Error("stored hash must be sha256 of the token")
	}
	tok2, _, _ := NewToken("ivs_")
	if tok == tok2 {
		t.Error("tokens must be random")
	}
}

func TestMaskSecret(t *testing.T) {
	tests := []struct{ in, want string }{
		{"sk-proj-abcdefghijklmnopWXYZ", "sk-proj-…WXYZ"},
		{"sk-ant-api03-abcdefgh1234", "sk-ant-…1234"},
		{"sk-abcdefghijklmn9876", "sk-…9876"},
		{"gsk_abcdefghijklmnop5555", "gsk_…5555"},
		{"AIzaSyA-abcdefghijklmn0000", "AIza…0000"},
		{"ghp_abcdefghijklmnop1111", "ghp_…1111"},
		{"plainsecretvalue42", "…ue42"},
		{"short", "••••"},
		{"  12345678  ", "••••"},
	}
	for _, tt := range tests {
		got := MaskSecret(tt.in)
		if got != tt.want {
			t.Errorf("MaskSecret(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if len(strings.TrimSpace(tt.in)) > 8 && strings.Contains(got, strings.TrimSpace(tt.in)[4:len(strings.TrimSpace(tt.in))-4]) {
			t.Errorf("MaskSecret(%q) leaks the middle of the secret: %q", tt.in, got)
		}
	}
}

func TestIsPublicAddr(t *testing.T) {
	tests := []struct {
		ip     string
		public bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"93.184.216.34", true},
		{"2606:4700:4700::1111", true},
		{"127.0.0.1", false},
		{"127.255.0.9", false},
		{"10.0.0.1", false},
		{"10.255.255.255", false},
		{"172.16.0.1", false},
		{"192.168.1.10", false},
		{"169.254.169.254", false}, // cloud metadata (AWS/GCP/Azure)
		{"169.254.170.2", false},   // ECS task metadata
		{"100.100.100.200", false}, // Alibaba metadata (CGNAT range)
		{"0.0.0.0", false},
		{"255.255.255.255", false},
		{"224.0.0.1", false},
		{"192.0.2.1", false},
		{"198.18.0.1", false},
		{"::1", false},
		{"::", false},
		{"fc00::1", false},
		{"fd00:ec2::254", false}, // AWS IPv6 metadata
		{"fe80::1", false},
		{"::ffff:127.0.0.1", false},
		{"::ffff:169.254.169.254", false},
		{"::ffff:10.0.0.1", false},
		{"64:ff9b::7f00:1", false}, // NAT64 of 127.0.0.1
		{"2002:7f00:1::", false},   // 6to4 of 127.0.0.1
		{"2001:db8::1", false},
	}
	for _, tt := range tests {
		ip := netip.MustParseAddr(tt.ip)
		if got := IsPublicAddr(ip); got != tt.public {
			t.Errorf("IsPublicAddr(%s) = %v, want %v", tt.ip, got, tt.public)
		}
	}
	if IsPublicAddr(netip.Addr{}) {
		t.Error("the zero address is not public")
	}
}

func TestSafeFetcherCheckURL(t *testing.T) {
	f := NewSafeFetcher(1<<20, 5*time.Second)
	tests := []struct {
		url string
		ok  bool
	}{
		{"https://example.com/page", true},
		{"http://example.com", true},
		{"ftp://example.com/file", false},
		{"file:///etc/passwd", false},
		{"gopher://example.com", false},
		{"javascript:alert(1)", false},
		{"http://localhost:8080/", false},
		{"http://LOCALHOST/", false},
		{"http://api.localhost/", false},
		{"http://printer.local/", false},
		{"http://metadata.google.internal/computeMetadata/v1/", false},
		{"http://127.0.0.1/", false},
		{"http://10.1.2.3/", false},
		{"http://169.254.169.254/latest/meta-data/", false},
		{"http://[::1]/", false},
		{"http://[fc00::1]/", false},
		{"http://user:pass@example.com/", false},
		{"http:///nohost", false},
	}
	for _, tt := range tests {
		u, err := url.Parse(tt.url)
		if err != nil {
			t.Fatalf("parse %s: %v", tt.url, err)
		}
		err = f.checkURL(u)
		if (err == nil) != tt.ok {
			t.Errorf("checkURL(%s) error = %v, want ok=%v", tt.url, err, tt.ok)
		}
	}
}

func TestSafeFetcherDialControl(t *testing.T) {
	f := NewSafeFetcher(1<<20, 5*time.Second)
	tests := []struct {
		addr string
		ok   bool
	}{
		{"93.184.216.34:443", true},
		{"93.184.216.34:80", true},
		{"93.184.216.34:22", false},   // only 80/443
		{"93.184.216.34:5432", false}, // no database ports
		{"127.0.0.1:443", false},      // DNS rebinding to loopback is caught at dial time
		{"10.0.0.5:80", false},
		{"169.254.169.254:80", false},
		{"[::1]:443", false},
		{"[fd00:ec2::254]:80", false},
		{"not-an-ip:80", false},
		{"missing-port", false},
	}
	for _, tt := range tests {
		err := f.control("tcp", tt.addr, nil)
		if (err == nil) != tt.ok {
			t.Errorf("control(%s) error = %v, want ok=%v", tt.addr, err, tt.ok)
		}
	}
	f.AllowPrivate = true
	if err := f.control("tcp", "127.0.0.1:5432", nil); err != nil {
		t.Errorf("AllowPrivate must disable address checks: %v", err)
	}
}

func TestSafeFetcherBlocksLocalServers(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("internal secret"))
	}))
	defer srv.Close()
	f := NewSafeFetcher(1<<20, 5*time.Second)
	ctx := context.Background()
	localhostURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	for _, u := range []string{srv.URL, localhostURL, "file:///etc/passwd", "ftp://127.0.0.1/x"} {
		body, _, _, err := f.Fetch(ctx, u)
		if err == nil {
			t.Errorf("Fetch(%s) succeeded with %q; must be blocked", u, body)
		}
	}
	if hits != 0 {
		t.Errorf("blocked fetches must never reach the server, got %d hits", hits)
	}
}

func TestSafeFetcherRedirectPolicy(t *testing.T) {
	f := NewSafeFetcher(1<<20, 5*time.Second)
	check := f.client.CheckRedirect
	req := func(raw string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	public := req("https://example.com/next")
	if err := check(public, []*http.Request{req("https://example.com/")}); err != nil {
		t.Errorf("public redirect rejected: %v", err)
	}
	for _, target := range []string{"http://169.254.169.254/latest/meta-data/", "http://localhost/admin", "http://10.0.0.1/", "file:///etc/passwd"} {
		if err := check(req(target), []*http.Request{req("https://example.com/")}); err == nil {
			t.Errorf("redirect to %s must be blocked", target)
		}
	}
	var via []*http.Request
	for i := 0; i < 5; i++ {
		via = append(via, req("https://example.com/"))
	}
	if err := check(public, via); err == nil {
		t.Error("more than 5 redirects must be refused")
	}
	if !errors.Is(f.checkURL(public.URL), nil) {
		t.Error("checkURL must accept public URLs")
	}
}

func TestSafeFetcherLimitsAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			_, _ = w.Write(bytes.Repeat([]byte("a"), 2048))
		case "/private":
			w.WriteHeader(http.StatusForbidden)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/boom":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("hello"))
		}
	}))
	defer srv.Close()
	f := NewSafeFetcher(1024, 5*time.Second)
	f.AllowPrivate = true // test server is on loopback
	f.AllowedPorts[strings.Split(strings.TrimPrefix(srv.URL, "http://"), ":")[1]] = true
	ctx := context.Background()
	body, ct, final, err := f.Fetch(ctx, srv.URL+"/ok")
	if err != nil || string(body) != "hello" || ct != "text/plain" || final != srv.URL+"/ok" {
		t.Fatalf("Fetch ok = %q %q %q %v", body, ct, final, err)
	}
	if _, _, _, err := f.Fetch(ctx, srv.URL+"/big"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("oversized response must fail, got %v", err)
	}
	if _, _, _, err := f.Fetch(ctx, srv.URL+"/private"); err == nil || !strings.Contains(err.Error(), "sign-in") {
		t.Errorf("403 must explain sign-in, got %v", err)
	}
	if _, _, _, err := f.Fetch(ctx, srv.URL+"/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404 must be explained, got %v", err)
	}
	if _, _, _, err := f.Fetch(ctx, srv.URL+"/boom"); err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("5xx must fail, got %v", err)
	}
}
