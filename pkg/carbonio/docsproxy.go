package carbonio

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// DocsProxy is a local-only HTTP reverse proxy that lets the desktop GUI
// embed Carbonio Docs Online inside its own window despite Wails v2 having
// no cookie-manager API (see https://github.com/wailsapp/wails/issues/2536,
// confirmed by the maintainers: "Cookies in webviews aren't handled
// automatically as they are not browsers"). There is no way to seed the
// ZM_AUTH_TOKEN cookie directly into the embedded webview's cookie jar for
// a third-party origin (https://<server>), and page JavaScript can never
// set document.cookie for an origin other than its own - that restriction
// is a browser-engine fundamental, not something Wails could work around.
//
// Instead, the GUI points an in-app <iframe> at this proxy's loopback
// address; the proxy owns the real, authenticated connection to the
// Carbonio server. It forces the ZM_AUTH_TOKEN cookie onto every
// forwarded request (mirroring newAuthenticatedClient/customTransport's
// cookie injection for the CLI/GUI's own API calls) and, on top of that,
// tracks every other cookie the server sets in a shared jar (see the jar
// field) - Carbonio Docs Online's "open" endpoint and the editor itself
// can each set cookies of their own that later requests then require,
// exactly like a real browser's cookie jar would carry them forward.
// From the webview's perspective, the whole Docs Online editor - its
// HTML page, JS/CSS assets, XHR API calls, and the WebSocket connection
// its collaborative editing relies on - simply lives at
// http://127.0.0.1:<port> and never needs a cookie of its own for the
// real server at all.
//
// A single DocsProxy is started lazily (see Start, called by
// App.OpenNodeWithDocs the first time a file is opened) and kept running
// for the GUI process' lifetime; SetCredentials updates which server/token
// it authenticates with without restarting the listener, e.g. after a
// re-login refreshes the token.
type DocsProxy struct {
	mu       sync.RWMutex
	endpoint string
	token    string

	// jar accumulates every cookie the real server sets - not just
	// ZM_AUTH_TOKEN - across both code paths that talk to it:
	// ResolveOpenURL's own request and every request the reverse proxy
	// forwards (see director/modifyResponse). Carbonio Docs Online's
	// "open" endpoint can itself hand back an editor/document-session
	// cookie that the editor's later requests then require; without a
	// shared jar, that cookie would be read once by ResolveOpenURL and
	// then silently dropped, breaking the embedded editor with an opaque
	// error even though authentication itself succeeded. A nil
	// PublicSuffixList is fine here: every cookie in play is scoped to
	// the single Carbonio endpoint this proxy talks to.
	jar *cookiejar.Jar

	startOnce sync.Once
	startErr  error
	baseURL   string
	server    *http.Server
}

// NewDocsProxy returns a DocsProxy ready to have its credentials set and
// be started.
func NewDocsProxy() *DocsProxy {
	jar, _ := cookiejar.New(nil) // nil PublicSuffixList never errors
	return &DocsProxy{jar: jar}
}

// SetCredentials updates the Carbonio server hostname and ZM_AUTH_TOKEN
// the proxy authenticates forwarded requests with. Safe to call
// concurrently with in-flight proxied requests (e.g. a re-login racing an
// already-open editor tab).
func (p *DocsProxy) SetCredentials(endpoint, token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.endpoint != endpoint {
		log.Debug().Str("endpoint", endpoint).Msg("[gui] docs proxy: credentials updated")
	}
	p.endpoint = endpoint
	p.token = token
}

// credentials returns the endpoint/token currently in effect.
func (p *DocsProxy) credentials() (endpoint, token string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.endpoint, p.token
}

// docsProxyMinPort and docsProxyMaxPort bound the local port range Start
// picks from (inclusive) for the reverse proxy listener.
const (
	docsProxyMinPort = 20000
	docsProxyMaxPort = 40000
)

// Start lazily starts the local proxy listener - bound to 127.0.0.1 only,
// on a port in [docsProxyMinPort, docsProxyMaxPort] chosen at random (see
// listenOnPortInRange), never reachable from outside the machine - and
// returns its base URL (e.g. "http://127.0.0.1:23456"). Subsequent calls
// are no-ops that return the same base URL: the same listener keeps
// serving every node opened for the rest of the process' life,
// SetCredentials (called before every Start) updating who it
// authenticates as.
func (p *DocsProxy) Start() (string, error) {
	p.startOnce.Do(func() {
		log.Debug().Msg("[gui] docs proxy: starting local reverse proxy listener")
		listener, err := listenOnPortInRange(docsProxyMinPort, docsProxyMaxPort)
		if err != nil {
			p.startErr = fmt.Errorf("docs proxy: listen: %w", err)
			log.Error().Err(err).Msg("[gui] docs proxy: failed to start local listener")
			return
		}
		p.baseURL = "http://" + listener.Addr().String()
		log.Debug().Str("baseURL", p.baseURL).Msg("[gui] docs proxy: local reverse proxy listening")
		p.server = &http.Server{Handler: p.reverseProxy()}
		go func() {
			if err := p.server.Serve(listener); err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("[gui] docs proxy stopped unexpectedly")
			}
		}()
	})
	return p.baseURL, p.startErr
}

// listenOnPortInRange binds a TCP listener on 127.0.0.1 to a port chosen
// at random from [min, max] (inclusive). Every port in the range is tried
// at most once, in random order, so a handful of ports already taken by
// other processes don't stop it from finding a free one; it only fails
// once the entire range has been exhausted.
func listenOnPortInRange(min, max int) (net.Listener, error) {
	var lastErr error
	for _, offset := range rand.Perm(max - min + 1) {
		port := min + offset
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return listener, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("no free port in [%d, %d]: %w", min, max, lastErr)
}

// Stop shuts down the local proxy listener, if it was ever started. Called
// once, on GUI shutdown.
func (p *DocsProxy) Stop() {
	if p.server != nil {
		log.Debug().Str("baseURL", p.baseURL).Msg("[gui] docs proxy: stopping local listener")
		_ = p.server.Close()
	}
}

// openWithDocsResponse mirrors the JSON body returned by
// GET /services/docs/files/open/<id> - see Carbonio's docs service and
// carbonio-files-ui's useOpenWithDocs hook
// (src/carbonio-files-ui-common/hooks/useOpenWithDocs.tsx), which
// ResolveOpenURL mirrors.
type openWithDocsResponse struct {
	FileOpenUrl string `json:"fileOpenUrl"`
}

// ResolveOpenURL performs the authenticated
// GET /services/docs/files/open/<nodeId> call directly against the real
// Carbonio server - same endpoint, same offset_from_utc query parameter
// (the client's UTC offset in minutes), same ZM_AUTH_TOKEN cookie
// authentication as newAuthenticatedClient - and returns its
// "fileOpenUrl" response field rewritten to this proxy's local base URL.
//
// That endpoint answers with JSON, not a redirect, so the embedded
// <iframe> can't just be pointed at it directly: the webview would render
// the raw {"fileOpenUrl": "..."} text instead of the editor. Resolving it
// here - the same way carbonio-files-ui's own JS does before navigating -
// means the frontend only ever hands the iframe a URL that already leads
// straight to the real editor.
//
// Any Set-Cookie the resolve response itself carries (Carbonio Docs
// Online's "open" endpoint can hand back an editor/document-session
// cookie of its own, separate from ZM_AUTH_TOKEN) is captured into the
// shared jar (see DocsProxy.jar), so the editor's subsequent requests -
// forwarded by director/modifyResponse - carry it too instead of the
// session silently missing a cookie the editor actually requires.
//
// Requires Start to have been called first (baseURL must already be set
// for the local rewrite).
func (p *DocsProxy) ResolveOpenURL(nodeId string) (string, error) {
	endpoint, token := p.credentials()

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	client := newAuthenticatedClient(dialer, token)

	_, offsetSeconds := time.Now().Zone()
	query := url.Values{"offset_from_utc": {strconv.Itoa(offsetSeconds / 60)}}

	openURL := "https://" + endpoint + "/services/docs/files/open/" + url.PathEscape(nodeId) + "?" + query.Encode()
	log.Debug().Str("nodeId", nodeId).Str("endpoint", endpoint).Str("url", openURL).
		Msg("[gui] docs proxy: resolving open-with-docs URL")

	resp, err := client.Get(openURL)
	if err != nil {
		log.Error().Err(err).Str("nodeId", nodeId).Str("url", openURL).
			Msg("[gui] docs proxy: resolve open url request failed")
		return "", fmt.Errorf("docs proxy: resolve open url: %w", err)
	}
	defer resp.Body.Close()

	if setCookies := resp.Cookies(); len(setCookies) > 0 {
		p.jar.SetCookies(resp.Request.URL, setCookies)
		log.Debug().Int("count", len(setCookies)).Msg("[gui] docs proxy: captured cookies from resolve response")
	}

	log.Debug().
		Str("nodeId", nodeId).
		Int("status", resp.StatusCode).
		Str("contentType", resp.Header.Get("Content-Type")).
		Str("contentEncoding", resp.Header.Get("Content-Encoding")).
		Msg("[gui] docs proxy: resolve open url response received")

	// decompressBody is required here: customTransport (newAuthenticatedClient)
	// advertises Accept-Encoding itself, which disables Go's automatic
	// response decompression (it only auto-decompresses when the caller
	// leaves Accept-Encoding unset) - feeding a gzip/br-compressed body
	// straight into the JSON decoder fails silently otherwise.
	body, readErr := io.ReadAll(io.LimitReader(decompressBody(resp), 1<<20))
	if readErr != nil {
		log.Error().Err(readErr).Str("nodeId", nodeId).Int("status", resp.StatusCode).
			Msg("[gui] docs proxy: failed to read resolve open url response body")
		return "", fmt.Errorf("docs proxy: resolve open url: read response: %w", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		log.Error().
			Str("nodeId", nodeId).
			Int("status", resp.StatusCode).
			Str("body", truncateForLog(body, 2048)).
			Msg("[gui] docs proxy: resolve open url returned a non-200 status")
		return "", fmt.Errorf("docs proxy: resolve open url: server returned %s", resp.Status)
	}

	var parsed openWithDocsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Error().
			Err(err).
			Str("nodeId", nodeId).
			Str("contentType", resp.Header.Get("Content-Type")).
			Str("contentEncoding", resp.Header.Get("Content-Encoding")).
			Str("body", truncateForLog(body, 2048)).
			Msg("[gui] docs proxy: failed to decode resolve open url response")
		return "", fmt.Errorf("docs proxy: resolve open url: decode response: %w", err)
	}
	if parsed.FileOpenUrl == "" {
		log.Error().Str("nodeId", nodeId).Str("body", truncateForLog(body, 2048)).
			Msg("[gui] docs proxy: resolve open url response has no fileOpenUrl")
		return "", errors.New("docs proxy: resolve open url: empty fileOpenUrl in response")
	}

	fileOpenUrl := parsed.FileOpenUrl
	if !strings.HasPrefix(fileOpenUrl, "http://") && !strings.HasPrefix(fileOpenUrl, "https://") {
		fileOpenUrl = "https://" + endpoint + fileOpenUrl
	}
	localURL := p.rewriteToLocal(fileOpenUrl)
	log.Debug().Str("nodeId", nodeId).Str("fileOpenUrl", fileOpenUrl).Str("localURL", localURL).
		Msg("[gui] docs proxy: resolved open-with-docs URL")
	return localURL, nil
}

// truncateForLog returns body as a string capped at max bytes, appending
// a marker when it was cut short, so a log line never balloons to the
// size of a full (possibly huge, possibly binary/mis-decoded) response
// body while still keeping enough of it to diagnose a failure.
func truncateForLog(body []byte, max int) string {
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + fmt.Sprintf("... (truncated, %d bytes total)", len(body))
}

// reverseProxy builds the httputil.ReverseProxy that forwards every
// request received on the local listener to the current endpoint,
// injecting the ZM_AUTH_TOKEN cookie.
func (p *DocsProxy) reverseProxy() http.Handler {
	transport := &http.Transport{
		// Carbonio servers commonly use a self-signed certificate (see
		// docs/notes.md); newAuthenticatedClient makes the same
		// trade-off for the CLI/GUI's own API calls.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return &httputil.ReverseProxy{
		Director:       p.director,
		Transport:      transport,
		ModifyResponse: p.modifyResponse,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error().Err(err).Str("method", r.Method).Str("path", r.URL.Path).Msg("[gui] docs proxy request failed")
			w.WriteHeader(http.StatusBadGateway)
		},
	}
}

// director rewrites an incoming http://127.0.0.1:<port>/<path> request
// into an authenticated https://<endpoint>/<path> one: same path/query,
// Host/SNI switched to endpoint, and the Cookie header replaced with the
// merge of every cookie DocsProxy has captured for that URL in its
// shared jar (see the jar field's doc comment - this is what carries a
// Docs Online editor/document-session cookie set during ResolveOpenURL,
// or by an earlier proxied response, into every later request) plus
// whatever the webview's own cookie jar already sent for 127.0.0.1,
// with ZM_AUTH_TOKEN always forced to the current token last - so a
// stale value either side might hold never wins. Origin/Referer, when
// present, are rewritten the same way so same-origin checks the server
// may perform against them still pass.
func (p *DocsProxy) director(req *http.Request) {
	endpoint, token := p.credentials()

	targetURL := &url.URL{Scheme: "https", Host: endpoint, Path: req.URL.Path, RawQuery: req.URL.RawQuery}
	merged := map[string]string{}
	for _, c := range p.jar.Cookies(targetURL) {
		merged[c.Name] = c.Value
	}
	for _, c := range req.Cookies() {
		merged[c.Name] = c.Value
	}
	merged["ZM_AUTH_TOKEN"] = token

	isUpgrade := strings.EqualFold(req.Header.Get("Connection"), "Upgrade") ||
		strings.Contains(strings.ToLower(req.Header.Get("Connection")), "upgrade")
	log.Debug().
		Str("method", req.Method).
		Str("path", req.URL.Path).
		Str("endpoint", endpoint).
		Int("cookieCount", len(merged)).
		Bool("isUpgradeRequest", isUpgrade).
		Str("upgradeType", req.Header.Get("Upgrade")).
		Msg("[gui] docs proxy: forwarding request")

	req.URL.Scheme = "https"
	req.URL.Host = endpoint
	req.Host = endpoint

	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	cookieParts := make([]string, 0, len(names))
	for _, name := range names {
		cookieParts = append(cookieParts, name+"="+merged[name])
	}
	req.Header.Set("Cookie", strings.Join(cookieParts, "; "))

	if req.Header.Get("Origin") != "" {
		req.Header.Set("Origin", "https://"+endpoint)
	}
	if ref := req.Header.Get("Referer"); ref != "" {
		req.Header.Set("Referer", p.rewriteToUpstream(ref))
	}
}

// modifyResponse rewrites responses coming back from the real server so
// the embedded webview - which only ever knows about
// http://127.0.0.1:<port> - can keep following the editor's own redirects
// and cookies, and so the embedding actually works at all:
//   - X-Frame-Options and the Content-Security-Policy frame-ancestors
//     directive are stripped. Carbonio Docs Online, like most editors,
//     ships anti-clickjacking headers that only permit being framed from
//     its own origin; since the GUI frames it from a different origin
//     (this local proxy), every webview honors that and silently refuses
//     to render the framed content at all - no visible error, just a
//     blank white iframe - unless these are removed.
//   - Location headers pointing back at https://<endpoint>/... are
//     rewritten to the local proxy's base URL, so a redirect the webview
//     follows keeps going through the proxy (and thus keeps getting the
//     ZM_AUTH_TOKEN cookie injected) instead of navigating straight to the
//     real server, cookie-less.
//   - Set-Cookie headers have their Domain and Secure attributes
//     stripped, so any cookie the editor itself sets is actually accepted
//     by the webview for 127.0.0.1 - a plain-HTTP loopback address -
//     instead of being silently dropped for targeting a Domain/Secure the
//     connection doesn't match.
//   - Text-ish bodies (HTML, JS, CSS, JSON, XML, plain text) have every
//     absolute https://<endpoint>/... or wss://<endpoint>/... reference
//     rewritten to this proxy's local base URL (see
//     rewriteBodyReferences) - Location-header rewriting alone only
//     covers HTTP redirects, but Collabora/OnlyOffice-style editors
//     commonly bake their own WebSocket endpoint as an absolute URL
//     directly into their bootstrap HTML/JS/config payload instead of
//     deriving it from the page's own origin. Left unrewritten, the
//     webview would open that WebSocket straight against the real
//     server - bypassing this proxy, and with it the only place
//     ZM_AUTH_TOKEN exists - and fail with an opaque
//     "failed to establish socket connection" error.
func (p *DocsProxy) modifyResponse(resp *http.Response) error {
	capturedCookies := resp.Cookies()
	if len(capturedCookies) > 0 {
		p.jar.SetCookies(resp.Request.URL, capturedCookies)
	}

	strippedXFrameOptions := resp.Header.Get("X-Frame-Options") != ""
	resp.Header.Del("X-Frame-Options")

	cspStripped := false
	if csp := resp.Header.Get("Content-Security-Policy"); csp != "" {
		if rewritten := stripFrameAncestors(csp); rewritten != "" {
			cspStripped = rewritten != csp
			resp.Header.Set("Content-Security-Policy", rewritten)
		} else {
			cspStripped = true
			resp.Header.Del("Content-Security-Policy")
		}
	}

	cookies := resp.Header.Values("Set-Cookie")
	if len(cookies) > 0 {
		rewritten := make([]string, len(cookies))
		for i, c := range cookies {
			rewritten[i] = stripCookieAttrs(c)
		}
		resp.Header.Del("Set-Cookie")
		for _, c := range rewritten {
			resp.Header.Add("Set-Cookie", c)
		}
	}

	location := resp.Header.Get("Location")
	if location != "" {
		resp.Header.Set("Location", p.rewriteToLocal(location))
	}

	bodyRewritten := false
	if isRewritableContentType(resp.Header.Get("Content-Type")) {
		rewrittenBody, changed, err := p.rewriteBodyReferences(resp)
		if err != nil {
			log.Error().Err(err).Str("path", resp.Request.URL.Path).
				Msg("[gui] docs proxy: failed to rewrite absolute server references in response body")
		} else {
			resp.Body = io.NopCloser(bytes.NewReader(rewrittenBody))
			resp.ContentLength = int64(len(rewrittenBody))
			resp.Header.Set("Content-Length", strconv.Itoa(len(rewrittenBody)))
			resp.Header.Del("Content-Encoding") // body is now decompressed plain text
			bodyRewritten = changed
		}
	}

	log.Debug().
		Str("path", resp.Request.URL.Path).
		Int("status", resp.StatusCode).
		Str("contentType", resp.Header.Get("Content-Type")).
		Bool("strippedXFrameOptions", strippedXFrameOptions).
		Bool("cspFrameAncestorsStripped", cspStripped).
		Int("setCookieCount", len(cookies)).
		Int("cookiesCapturedIntoJar", len(capturedCookies)).
		Bool("locationRewritten", location != "").
		Bool("bodyRewritten", bodyRewritten).
		Msg("[gui] docs proxy: response received")

	return nil
}

// maxRewritableBodySize bounds how much of a text-ish response body
// rewriteBodyReferences buffers in memory to find (and replace) absolute
// references to the real server. Payloads that actually need this
// rewrite - the editor's bootstrap HTML page, its discovery/config JSON,
// its JS bundles - are, in practice, at most a few MB; binary content
// (documents, fonts, images, WASM) never matches isRewritableContentType
// and streams through untouched regardless of size. 64 MiB is a generous
// ceiling against a mislabeled response.
const maxRewritableBodySize = 64 << 20

// rewritableContentTypePrefixes lists the Content-Type prefixes whose
// response bodies rewriteBodyReferences scans. Binary content types
// (documents, images, fonts, wasm, ...) are deliberately excluded: they
// never embed a textual server reference, and buffering/rewriting them
// would be wasted work at best and a data-corrupting bug at worst.
var rewritableContentTypePrefixes = []string{
	"text/html",
	"text/javascript",
	"application/javascript",
	"application/json",
	"text/json",
	"text/css",
	"application/xml",
	"text/xml",
	"text/plain",
}

// isRewritableContentType reports whether contentType (a raw Content-Type
// header value, parameters and all) matches one of
// rewritableContentTypePrefixes.
func isRewritableContentType(contentType string) bool {
	ct := strings.ToLower(contentType)
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		ct = ct[:idx]
	}
	ct = strings.TrimSpace(ct)
	for _, prefix := range rewritableContentTypePrefixes {
		if strings.HasPrefix(ct, prefix) {
			return true
		}
	}
	return false
}

// rewriteBodyReferences rewrites every absolute https://<endpoint>/... or
// wss://<endpoint>/... reference embedded in a text-ish response body to
// point at this proxy's local base URL instead - both a plain and a
// JSON-escaped ("\/") form of the slashes, since a URL landing inside a
// JSON string literal commonly carries escaped slashes. It always fully
// reads (decompressing per Content-Encoding) and closes resp.Body; the
// caller must replace resp.Body with the returned bytes regardless of
// whether anything actually changed.
//
// This exists on top of the Location-header rewriting in modifyResponse
// because Location only covers HTTP redirects: Carbonio Docs Online's
// editor (Collabora/OnlyOffice-style) commonly bakes its own WebSocket
// endpoint as an absolute URL directly into its bootstrap HTML/JS/config
// payload rather than deriving it from the current page's origin.
func (p *DocsProxy) rewriteBodyReferences(resp *http.Response) ([]byte, bool, error) {
	endpoint, _ := p.credentials()

	body, err := io.ReadAll(io.LimitReader(decompressBody(resp), maxRewritableBodySize))
	closeErr := resp.Body.Close()
	if err != nil {
		return nil, false, err
	}
	if closeErr != nil {
		return nil, false, closeErr
	}

	wsBase := "ws://" + strings.TrimPrefix(p.baseURL, "http://")
	type replacement struct{ from, to string }
	replacements := []replacement{
		{"https://" + endpoint, p.baseURL},
		{"wss://" + endpoint, wsBase},
		{strings.ReplaceAll("https://"+endpoint, "/", `\/`), strings.ReplaceAll(p.baseURL, "/", `\/`)},
		{strings.ReplaceAll("wss://"+endpoint, "/", `\/`), strings.ReplaceAll(wsBase, "/", `\/`)},
	}

	rewritten := body
	for _, r := range replacements {
		rewritten = bytes.ReplaceAll(rewritten, []byte(r.from), []byte(r.to))
	}
	return rewritten, !bytes.Equal(rewritten, body), nil
}

// stripFrameAncestors removes the frame-ancestors directive from a
// Content-Security-Policy header value, leaving every other directive
// untouched. Returns "" if nothing is left worth keeping.
func stripFrameAncestors(csp string) string {
	directives := strings.Split(csp, ";")
	kept := make([]string, 0, len(directives))
	for _, d := range directives {
		trimmed := strings.TrimSpace(d)
		if trimmed == "" || strings.HasPrefix(strings.ToLower(trimmed), "frame-ancestors") {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, "; ")
}

// rewriteToLocal rewrites an absolute https://<endpoint>/... URL to the
// local proxy's base URL; anything else (relative paths, external hosts)
// is returned unchanged - a relative path already resolves against the
// webview's current http://127.0.0.1:<port> origin on its own.
func (p *DocsProxy) rewriteToLocal(rawURL string) string {
	endpoint, _ := p.credentials()
	if strings.HasPrefix(rawURL, "https://"+endpoint) {
		return p.baseURL + strings.TrimPrefix(rawURL, "https://"+endpoint)
	}
	return rawURL
}

// rewriteToUpstream is rewriteToLocal's inverse, used for the Referer
// header on outgoing requests (the previous page the webview navigated
// from, expressed as one of the proxy's own local URLs).
func (p *DocsProxy) rewriteToUpstream(rawURL string) string {
	endpoint, _ := p.credentials()
	if strings.HasPrefix(rawURL, p.baseURL) {
		return "https://" + endpoint + strings.TrimPrefix(rawURL, p.baseURL)
	}
	return rawURL
}

// stripCookieAttrs removes the Domain and Secure attributes from a single
// Set-Cookie header value, leaving the name=value pair and every other
// attribute (Path, Max-Age, SameSite, HttpOnly, ...) untouched.
func stripCookieAttrs(setCookie string) string {
	parts := strings.Split(setCookie, ";")
	kept := parts[:1] // "name=value" is always kept
	for _, attr := range parts[1:] {
		lower := strings.ToLower(strings.TrimSpace(attr))
		if lower == "secure" || strings.HasPrefix(lower, "domain=") {
			continue
		}
		kept = append(kept, attr)
	}
	return strings.Join(kept, ";")
}
