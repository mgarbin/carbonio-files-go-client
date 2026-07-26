package carbonio

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestNewDocsProxy_InitialState locks in NewDocsProxy's zero-value contract:
// the returned proxy must already own a working cookie jar (director and
// ResolveOpenURL both call p.jar.SetCookies/Cookies unconditionally, so a
// nil jar would panic on first use) and must start with no credentials set.
func TestNewDocsProxy_InitialState(t *testing.T) {
	p := NewDocsProxy()
	if p.jar == nil {
		t.Fatal("NewDocsProxy() jar = nil, want a non-nil cookie jar")
	}
	endpoint, token := p.credentials()
	if endpoint != "" || token != "" {
		t.Fatalf("NewDocsProxy() credentials = (%q, %q), want (\"\", \"\")", endpoint, token)
	}
}

// TestDocsProxy_SetCredentials_RoundTrip verifies SetCredentials/credentials
// simply store and return exactly the endpoint/token pair given to them.
func TestDocsProxy_SetCredentials_RoundTrip(t *testing.T) {
	p := NewDocsProxy()
	p.SetCredentials("mail.example.com", "tok-abc")
	endpoint, token := p.credentials()
	if endpoint != "mail.example.com" || token != "tok-abc" {
		t.Fatalf("credentials() = (%q, %q), want (%q, %q)", endpoint, token, "mail.example.com", "tok-abc")
	}

	// A later call must overwrite, not merge with, the previous value.
	p.SetCredentials("other.example.com", "tok-xyz")
	endpoint, token = p.credentials()
	if endpoint != "other.example.com" || token != "tok-xyz" {
		t.Fatalf("credentials() after update = (%q, %q), want (%q, %q)", endpoint, token, "other.example.com", "tok-xyz")
	}
}

// TestDocsProxy_SetCredentials_ConcurrentAccess guards the mutex protecting
// endpoint/token: SetCredentials is documented as safe to call while a
// request is in flight (e.g. a re-login racing an already-open editor tab),
// so concurrent writers and readers must never race or corrupt the pair.
// Run with -race to catch a regression.
func TestDocsProxy_SetCredentials_ConcurrentAccess(t *testing.T) {
	p := NewDocsProxy()
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := range 50 {
				p.SetCredentials(fmt.Sprintf("host-%d.example.com", i), fmt.Sprintf("tok-%d-%d", i, j))
			}
		}(i)
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_, _ = p.credentials()
			}
		}()
	}
	wg.Wait()

	// After the dust settles, the pair must still be internally
	// consistent (some (endpoint, token) that was actually set together,
	// not a torn read).
	endpoint, token := p.credentials()
	if !strings.HasPrefix(endpoint, "host-") || !strings.Contains(token, "tok-") {
		t.Fatalf("credentials() after concurrent writes = (%q, %q), looks torn", endpoint, token)
	}
}

// TestDocsProxy_Start_ReturnsLoopbackURLInPortRange checks Start's documented
// contract: a "http://127.0.0.1:<port>" base URL with the port drawn from
// [docsProxyMinPort, docsProxyMaxPort].
func TestDocsProxy_Start_ReturnsLoopbackURLInPortRange(t *testing.T) {
	p := NewDocsProxy()
	t.Cleanup(p.Stop)

	baseURL, err := p.Start()
	if err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	re := regexp.MustCompile(`^http://127\.0\.0\.1:(\d+)$`)
	m := re.FindStringSubmatch(baseURL)
	if m == nil {
		t.Fatalf("Start() baseURL = %q, want to match %s", baseURL, re.String())
	}
	port, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("parsing port from %q: %v", baseURL, err)
	}
	if port < docsProxyMinPort || port > docsProxyMaxPort {
		t.Fatalf("Start() port = %d, want in [%d, %d]", port, docsProxyMinPort, docsProxyMaxPort)
	}
}

// TestDocsProxy_Start_IsIdempotent verifies the startOnce guard: a second
// Start() call must be a no-op returning the exact same base URL, not bind a
// second listener on a different port.
func TestDocsProxy_Start_IsIdempotent(t *testing.T) {
	p := NewDocsProxy()
	t.Cleanup(p.Stop)

	first, err := p.Start()
	if err != nil {
		t.Fatalf("first Start() error = %v, want nil", err)
	}
	second, err := p.Start()
	if err != nil {
		t.Fatalf("second Start() error = %v, want nil", err)
	}
	if first != second {
		t.Fatalf("second Start() = %q, want the same URL as the first Start() = %q", second, first)
	}
}

// TestDocsProxy_Stop_ShutsDownListener verifies Stop actually tears the
// listener down: a request to the base URL afterwards must fail instead of
// silently continuing to serve.
func TestDocsProxy_Stop_ShutsDownListener(t *testing.T) {
	p := NewDocsProxy()
	baseURL, err := p.Start()
	if err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	// Sanity: the listener actually accepts connections before Stop.
	if _, err := http.Get(baseURL + "/"); err != nil {
		t.Fatalf("request before Stop() unexpectedly failed: %v", err)
	}

	p.Stop()

	if _, err := http.Get(baseURL + "/"); err == nil {
		t.Fatal("request after Stop() succeeded, want connection error")
	}
}

// TestListenOnPortInRange_TinyRange checks that a single-port range still
// yields a working listener bound to that exact port on 127.0.0.1.
func TestListenOnPortInRange_TinyRange(t *testing.T) {
	// Grab a currently-free ephemeral port, release it, then hand
	// listenOnPortInRange a range containing only that one port.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probing for a free port: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatalf("closing probe listener: %v", err)
	}

	listener, err := listenOnPortInRange(port, port)
	if err != nil {
		t.Fatalf("listenOnPortInRange(%d, %d) error = %v, want nil", port, port, err)
	}
	defer listener.Close()

	got := listener.Addr().(*net.TCPAddr)
	if got.Port != port {
		t.Fatalf("listenOnPortInRange(%d, %d) bound port %d, want %d", port, port, got.Port, port)
	}
	if !got.IP.IsLoopback() {
		t.Fatalf("listenOnPortInRange(%d, %d) bound IP %v, want a loopback address", port, port, got.IP)
	}

	// The listener must genuinely work: a client can connect to it.
	conn, err := net.Dial("tcp", got.String())
	if err != nil {
		t.Fatalf("dialing listener at %v: %v", got, err)
	}
	conn.Close()
}

// TestIsRewritableContentType locks in the exact matching rule against
// rewritableContentTypePrefixes: lower-cased, parameters (";...") stripped,
// then a prefix match - so "text/html; charset=utf-8" counts but a binary
// type never does, regardless of casing.
func TestIsRewritableContentType(t *testing.T) {
	cases := map[string]bool{
		"text/html":                       true,
		"text/html; charset=utf-8":        true,
		"TEXT/HTML; charset=UTF-8":        true,
		"text/javascript; charset=utf-8":  true,
		"application/javascript":          true,
		"application/json":                true,
		"application/json; charset=utf-8": true,
		"text/json":                       true,
		"text/css":                        true,
		"application/xml":                 true,
		"text/xml":                        true,
		"text/plain; charset=utf-8":       true,
		"application/octet-stream":        false,
		"image/png":                       false,
		"font/woff2":                      false,
		"application/wasm":                false,
		"application/pdf":                 false,
		"":                                false,
		"text":                            false, // no slash, not an exact prefix hit either
		"texture/foo":                     false, // must not fuzzily match "text"
	}
	for contentType, want := range cases {
		if got := isRewritableContentType(contentType); got != want {
			t.Errorf("isRewritableContentType(%q) = %v, want %v", contentType, got, want)
		}
	}
}

// TestStripFrameAncestors_MixedDirectives verifies only the frame-ancestors
// directive is dropped from a multi-directive CSP; the rest survive with
// the source's canonical "; " join, in their original order.
func TestStripFrameAncestors_MixedDirectives(t *testing.T) {
	csp := "default-src 'self'; frame-ancestors 'self'; script-src 'unsafe-inline'"
	want := "default-src 'self'; script-src 'unsafe-inline'"
	if got := stripFrameAncestors(csp); got != want {
		t.Fatalf("stripFrameAncestors(%q) = %q, want %q", csp, got, want)
	}
}

// TestStripFrameAncestors_NoFrameAncestors verifies a CSP that never
// mentions frame-ancestors passes through unchanged, as long as its
// directives are already separated the canonical "; " way the source
// rejoins with.
func TestStripFrameAncestors_NoFrameAncestors(t *testing.T) {
	csp := "default-src 'self'; script-src 'unsafe-inline'"
	if got := stripFrameAncestors(csp); got != csp {
		t.Fatalf("stripFrameAncestors(%q) = %q, want unchanged %q", csp, got, csp)
	}
}

// TestStripFrameAncestors_OnlyFrameAncestors verifies that when
// frame-ancestors is the only directive present, nothing is left worth
// keeping and the function returns "" per its doc comment.
func TestStripFrameAncestors_OnlyFrameAncestors(t *testing.T) {
	csp := "frame-ancestors 'self'"
	if got := stripFrameAncestors(csp); got != "" {
		t.Fatalf("stripFrameAncestors(%q) = %q, want \"\"", csp, got)
	}
}

// TestDocsProxy_RewriteToLocalAndUpstream_RoundTrip verifies rewriteToLocal
// rewrites an absolute upstream URL to the proxy's local base URL and
// rewriteToUpstream is its exact inverse, once both endpoint and baseURL are
// known (SetCredentials + Start).
func TestDocsProxy_RewriteToLocalAndUpstream_RoundTrip(t *testing.T) {
	p := NewDocsProxy()
	t.Cleanup(p.Stop)
	p.SetCredentials("mail.example.com", "tok-123")
	baseURL, err := p.Start()
	if err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	upstream := "https://mail.example.com/foo"
	local := p.rewriteToLocal(upstream)
	wantLocal := baseURL + "/foo"
	if local != wantLocal {
		t.Fatalf("rewriteToLocal(%q) = %q, want %q", upstream, local, wantLocal)
	}

	roundTripped := p.rewriteToUpstream(local)
	if roundTripped != upstream {
		t.Fatalf("rewriteToUpstream(%q) = %q, want %q", local, roundTripped, upstream)
	}
}

// TestDocsProxy_RewriteToLocal_LeavesUnrelatedURLsAlone verifies a relative
// path and a URL for a different host both pass through rewriteToLocal
// unchanged, since only an absolute reference to the current endpoint is
// eligible for rewriting.
func TestDocsProxy_RewriteToLocal_LeavesUnrelatedURLsAlone(t *testing.T) {
	p := NewDocsProxy()
	p.SetCredentials("mail.example.com", "tok-123")
	if _, err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	t.Cleanup(p.Stop)

	cases := []string{
		"/foo/bar?x=1",
		"https://other.example.com/foo",
	}
	for _, rawURL := range cases {
		if got := p.rewriteToLocal(rawURL); got != rawURL {
			t.Errorf("rewriteToLocal(%q) = %q, want unchanged %q", rawURL, got, rawURL)
		}
	}
}

// TestDocsProxy_RewriteToUpstream_LeavesUnrelatedURLsAlone mirrors the
// previous case for rewriteToUpstream: anything not prefixed by the
// proxy's own baseURL must be returned as-is.
func TestDocsProxy_RewriteToUpstream_LeavesUnrelatedURLsAlone(t *testing.T) {
	p := NewDocsProxy()
	p.SetCredentials("mail.example.com", "tok-123")
	if _, err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	t.Cleanup(p.Stop)

	rawURL := "https://somewhere-else.example.com/foo"
	if got := p.rewriteToUpstream(rawURL); got != rawURL {
		t.Fatalf("rewriteToUpstream(%q) = %q, want unchanged %q", rawURL, got, rawURL)
	}
}

// TestStripCookieAttrs removes exactly Domain and Secure from a Set-Cookie
// value (case-insensitively), keeping name=value and every other attribute
// (Path, HttpOnly, SameSite, Max-Age) byte-for-byte, including their
// original separators.
func TestStripCookieAttrs(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			in:   "sid=abc123; Domain=example.com; Path=/; Secure; HttpOnly",
			want: "sid=abc123; Path=/; HttpOnly",
		},
		{
			// Case-insensitive attribute names/values.
			in:   "sid=abc123; SECURE; domain=EXAMPLE.com; SameSite=Lax",
			want: "sid=abc123; SameSite=Lax",
		},
		{
			// No Domain/Secure present: untouched.
			in:   "sid=abc123; Path=/; HttpOnly",
			want: "sid=abc123; Path=/; HttpOnly",
		},
		{
			// Only name=value, nothing else.
			in:   "sid=abc123",
			want: "sid=abc123",
		},
	}
	for _, c := range cases {
		if got := stripCookieAttrs(c.in); got != c.want {
			t.Errorf("stripCookieAttrs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// newDocsProxyUpstreamServer starts a self-signed TLS server standing in for
// a real Carbonio endpoint and returns its "host:port", matching the
// endpoint format SetCredentials expects (production code always prepends
// "https://"). The reverse proxy's transport sets InsecureSkipVerify, so a
// self-signed cert must not block forwarding - exactly the trade-off
// newAuthenticatedClient already makes for the CLI/GUI's own API calls.
func newDocsProxyUpstreamServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

// TestDocsProxy_EndToEnd_ForwardsAuthAndBody spins up a fake upstream
// standing in for the real Carbonio server, points a DocsProxy at it, and
// drives a plain HTTP request through the local proxy exactly like the
// embedded webview would. It asserts: (1) the proxy accepts the upstream's
// self-signed certificate (InsecureSkipVerify in reverseProxy's transport),
// (2) the ZM_AUTH_TOKEN cookie set via SetCredentials is injected into the
// forwarded request even though the client sent no cookie at all, and (3)
// a non-rewritable Content-Type response body is streamed back unchanged.
func TestDocsProxy_EndToEnd_ForwardsAuthAndBody(t *testing.T) {
	const upstreamBody = "\x00\x01binary-ish-payload\xff"

	var (
		mu        sync.Mutex
		gotPath   string
		gotCookie string
	)
	endpoint := newDocsProxyUpstreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		gotCookie = r.Header.Get("Cookie")
		mu.Unlock()

		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(upstreamBody))
	})

	p := NewDocsProxy()
	t.Cleanup(p.Stop)
	p.SetCredentials(endpoint, "tok-123")
	baseURL, err := p.Start()
	if err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	resp, err := http.Get(baseURL + "/some/path")
	if err != nil {
		t.Fatalf("GET %s/some/path: %v", baseURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading proxied response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxied response status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if string(body) != upstreamBody {
		t.Fatalf("proxied response body = %q, want unchanged %q", body, upstreamBody)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/some/path" {
		t.Fatalf("upstream received path = %q, want %q", gotPath, "/some/path")
	}
	if !strings.Contains(gotCookie, "ZM_AUTH_TOKEN=tok-123") {
		t.Fatalf("upstream received Cookie = %q, want it to contain ZM_AUTH_TOKEN=tok-123", gotCookie)
	}
}
