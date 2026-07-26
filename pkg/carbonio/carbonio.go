package carbonio

import (
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Authenticator interface {
	// Authenticate performs an HTTP POST with email and password,
	// returns the value of the ZM_AUTH_TOKEN cookie if authentication succeeds.
	CarbonioZxAuth(email, password string) (string, error)
	DownloadFile(token, nodeId, destPath string, fileSize int64, maxRetries int) error
	UploadFile(token, parentId, filePath string, newVersion, overWriteVersion bool, nodeId *string) (string, error)
}

// AuthErrorKind classifies the outcome of a failed CarbonioZxAuth call so
// callers (in particular the GUI) can react without parsing message
// strings. The classification only distinguishes what the /zx/auth/v2/login
// endpoint actually exposes over HTTP: PasswordAuthManager/LdapProvisioning
// wrap every authentication failure (bad password, locked/inactive/
// maintenance-mode account, ...) into a generic AuthenticationError that the
// server maps to a bare 401 with no body, so those cases are indistinguishable
// on the wire and all surface as AuthErrorInvalidCredentials.
type AuthErrorKind string

const (
	// AuthErrorInvalidInput means the email/password never left the client
	// (e.g. malformed email address).
	AuthErrorInvalidInput AuthErrorKind = "invalid_input"
	// AuthErrorInvalidCredentials covers every 401 the login endpoint can
	// return: wrong password, locked/inactive account, maintenance mode.
	AuthErrorInvalidCredentials AuthErrorKind = "invalid_credentials"
	// AuthErrorMustChangePassword is the one login failure the server does
	// single out: a 3xx redirect to the change-password page
	// (loginErrorCode=account.CHANGE_PASSWORD).
	AuthErrorMustChangePassword AuthErrorKind = "must_change_password"
	// AuthErrorForbidden is a 403 (e.g. IP/domain not authorized).
	AuthErrorForbidden AuthErrorKind = "forbidden"
	// AuthErrorBadRequest is a 400 (malformed login payload).
	AuthErrorBadRequest AuthErrorKind = "bad_request"
	// AuthErrorServer is any 5xx from the server.
	AuthErrorServer AuthErrorKind = "server_error"
	// AuthErrorNetwork means the request never got a response (DNS, refused
	// connection, TLS failure, timeout, ...).
	AuthErrorNetwork AuthErrorKind = "network"
	// AuthErrorUnknown is anything not covered above (unexpected status
	// code, missing cookie on an otherwise-200 response, ...).
	AuthErrorUnknown AuthErrorKind = "unknown"
)

// AuthError is the error type returned by CarbonioZxAuth on failure.
type AuthError struct {
	Kind       AuthErrorKind
	StatusCode int
	Detail     string
}

func (e *AuthError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("carbonio auth error [%s]: %s (HTTP %d)", e.Kind, e.Detail, e.StatusCode)
	}
	return fmt.Sprintf("carbonio auth error [%s]: %s", e.Kind, e.Detail)
}

// logWithAuthError attaches err to a zerolog event and, when err is an
// *AuthError (the type every carbonio auth call returns on failure), adds
// its Kind and, if present, the HTTP StatusCode the server responded with
// as their own structured fields. Str/Int fields are queryable in JSON log
// output, so callers no longer have to parse AuthError.Error()'s formatted
// string to see exactly what the server sent back.
func logWithAuthError(ev *zerolog.Event, err error) *zerolog.Event {
	ev = ev.Err(err)
	var authErr *AuthError
	if errors.As(err, &authErr) {
		ev = ev.Str("authErrorKind", string(authErr.Kind))
		if authErr.StatusCode != 0 {
			ev = ev.Int("statusCode", authErr.StatusCode)
		}
	}
	return ev
}

type HTTPAuthenticator struct {
	Endpoint string
}

// customTransport adds the Cookie header (plus a few browser-like headers)
// to every request. TLS/dialer settings live on base (see
// newAuthenticatedClient), never on this struct - a field here would be
// silently ignored, since RoundTrip only ever delegates to base.
type customTransport struct {
	base      http.RoundTripper
	authToken string
}

// ProgressWriter wraps an io.Writer and displays progress.
type ProgressWriter struct {
	Writer      io.Writer
	Total       int64 // expected size
	Downloaded  int64 // bytes written
	LastPrinted int64
	FileName    string
}

// Write reports download progress via a live, carriage-return-redrawn
// terminal bar. This intentionally stays on fmt/stdout instead of zerolog:
// zerolog emits one line per call (with timestamp/level framing), which
// would turn an in-place progress bar into hundreds of scrolling log lines.
func (pw *ProgressWriter) Write(p []byte) (n int, err error) {
	n, err = pw.Writer.Write(p)
	pw.Downloaded += int64(n)
	// Print every 1% or when finished
	percent := int(float64(pw.Downloaded) / float64(pw.Total) * 100)
	lastPercent := int(float64(pw.LastPrinted) / float64(pw.Total) * 100)
	if pw.Total > 0 && (percent > lastPercent || pw.Downloaded == pw.Total) {
		fmt.Printf("\r%s: [%-50s] %3d%%", pw.FileName, progressBar(percent), percent)
		pw.LastPrinted = pw.Downloaded
		if pw.Downloaded == pw.Total {
			fmt.Println()
		}
	}
	return
}

func progressBar(percent int) string {
	bars := percent / 2
	return fmt.Sprintf("%s%s", stringRepeat("=", bars), stringRepeat(" ", 50-bars))
}

func stringRepeat(s string, count int) string {
	res := ""
	for i := 0; i < count; i++ {
		res += s
	}
	return res
}

func (t *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cookieValue := fmt.Sprintf("ZM_AUTH_TOKEN=%s", t.authToken)
	req.Header.Set("Cookie", cookieValue)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Priority", "u=0, i")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("TE", "trailers")
	return t.base.RoundTrip(req)
}

// newAuthenticatedClient builds an http.Client that authenticates every
// request via the ZM_AUTH_TOKEN cookie (plus a few browser-like headers)
// and talks to the Carbonio server over TLS without verifying its
// certificate (Carbonio servers commonly use a self-signed certificate;
// see docs/notes.md).
func newAuthenticatedClient(dialer *net.Dialer, authToken string) *http.Client {
	return &http.Client{
		Transport: &customTransport{
			base: &http.Transport{
				DialContext:           dialer.DialContext,
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
				ExpectContinueTimeout: 1 * time.Second,
			},
			authToken: authToken,
		},
	}
}

func isValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// Sha384Base64 takes a file path, computes its SHA-384 hash, and returns the hash in base64 encoding.
func Sha384Base64(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha512.New384()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	hash := hasher.Sum(nil) // []byte, binary SHA-384

	return base64.StdEncoding.EncodeToString(hash), nil
}

// DetectMimeType returns the MIME type of the given file.
func DetectMimeType(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil {
		return "", err
	}

	mimeType := http.DetectContentType(buffer[:n])
	return mimeType, nil
}

// ExtractFileName takes a file path and returns the base file name.
func ExtractFileName(filePath string) string {
	return filepath.Base(filePath)
}

// GetFileContentLength returns the size of the file in bytes.
func GetFileContentLength(filePath string) (int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (a *HTTPAuthenticator) CarbonioZxAuth(email, password string) (*string, error) {
	// Verify if email respect rfc
	if !isValidEmail(email) {
		return nil, &AuthError{Kind: AuthErrorInvalidInput, Detail: "invalid email address format"}
	}

	// Create payload to inject to zx auth endpoint
	payload := map[string]string{
		"auth_method": "password",
		"user":        email,
		"password":    password,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &AuthError{Kind: AuthErrorUnknown, Detail: err.Error()}
	}

	// Make the request
	req, err := http.NewRequest("POST", "https://"+a.Endpoint+"/zx/auth/v2/login", bytes.NewBuffer(body))
	if err != nil {
		return nil, &AuthError{Kind: AuthErrorUnknown, Detail: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")

	// Create HTTP client with SSL verification disabled. Redirects are not
	// followed: a 3xx response to the login itself is how the server signals
	// "password must be changed", and following it would hide that behind a
	// generic "cookie not found" error.
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Wait for response
	resp, err := client.Do(req)
	if err != nil {
		return nil, &AuthError{Kind: AuthErrorNetwork, Detail: err.Error()}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "ZM_AUTH_TOKEN" {
				token := cookie.Value
				return &token, nil
			}
		}
		return nil, &AuthError{Kind: AuthErrorUnknown, StatusCode: resp.StatusCode, Detail: "ZM_AUTH_TOKEN cookie not found"}
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		location := resp.Header.Get("Location")
		if strings.Contains(location, "loginErrorCode=account.CHANGE_PASSWORD") {
			return nil, &AuthError{Kind: AuthErrorMustChangePassword, StatusCode: resp.StatusCode, Detail: "password must be changed"}
		}
		return nil, &AuthError{Kind: AuthErrorUnknown, StatusCode: resp.StatusCode, Detail: "unexpected redirect: " + location}
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, &AuthError{Kind: AuthErrorInvalidCredentials, StatusCode: resp.StatusCode, Detail: "invalid username or password"}
	case resp.StatusCode == http.StatusForbidden:
		return nil, &AuthError{Kind: AuthErrorForbidden, StatusCode: resp.StatusCode, Detail: "access forbidden"}
	case resp.StatusCode == http.StatusBadRequest:
		return nil, &AuthError{Kind: AuthErrorBadRequest, StatusCode: resp.StatusCode, Detail: "bad request"}
	case resp.StatusCode >= 500:
		return nil, &AuthError{Kind: AuthErrorServer, StatusCode: resp.StatusCode, Detail: "server error"}
	default:
		return nil, &AuthError{Kind: AuthErrorUnknown, StatusCode: resp.StatusCode, Detail: fmt.Sprintf("unexpected status %d", resp.StatusCode)}
	}
}

// TokenStatus classifies the outcome of ValidateToken.
type TokenStatus string

const (
	// TokenValid means the server still accepts the token as-is.
	TokenValid TokenStatus = "valid"
	// TokenInvalid means the server no longer accepts the token (expired,
	// deregistered/logged out elsewhere, or malformed): the caller must
	// fall back to a full username/password login.
	TokenInvalid TokenStatus = "invalid"
)

// ValidateToken checks whether token is still accepted by the server,
// without performing a full username/password login. It calls
// GET /zx/auth/v2/myself - the same versioned endpoint /zx/auth/v2/login
// lives under - passing token as the ZM_AUTH_TOKEN cookie. Carbonio's
// AuthorizedApiHandler (see carbonio-auth's MyselfAuthHandler) maps every
// reason a token stops being usable (expired, deregistered, malformed) to
// a bare 401 with no body, so a 401 here is the correct, and only, signal
// to fall back to a fresh login; anything else (network error, 5xx, ...)
// is returned as an error and must not be treated as "invalid".
func (a *HTTPAuthenticator) ValidateToken(token string) (TokenStatus, error) {
	if token == "" {
		return TokenInvalid, nil
	}

	log.Debug().Str("endpoint", a.Endpoint).Msg("[auth] validating cached auth token against server")

	req, err := http.NewRequest("GET", "https://"+a.Endpoint+"/zx/auth/v2/myself", nil)
	if err != nil {
		return "", &AuthError{Kind: AuthErrorUnknown, Detail: err.Error()}
	}
	req.Header.Set("Cookie", fmt.Sprintf("ZM_AUTH_TOKEN=%s", token))

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Warn().Err(err).Str("endpoint", a.Endpoint).Msg("[auth] cached auth token validation request failed")
		return "", &AuthError{Kind: AuthErrorNetwork, Detail: err.Error()}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		log.Debug().Msg("[auth] server accepted cached auth token")
		return TokenValid, nil
	case http.StatusUnauthorized:
		log.Debug().Msg("[auth] server rejected cached auth token with 401, it is no longer usable")
		return TokenInvalid, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Warn().
			Int("statusCode", resp.StatusCode).
			Str("body", string(body)).
			Msg("[auth] server returned an unexpected status validating the cached auth token")
		return "", &AuthError{Kind: AuthErrorUnknown, StatusCode: resp.StatusCode, Detail: fmt.Sprintf("unexpected status validating token: %d", resp.StatusCode)}
	}
}

func (a *HTTPAuthenticator) DownloadFile(token, nodeId, destPath, fileName string, fileSize int64, maxRetries int, wg *sync.WaitGroup, sem chan struct{}) (*string, error) {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second, // Only dial (connection) timeout
	}

	// Optionally, set up an authenticated HTTP client
	httpClient := newAuthenticatedClient(dialer, token)

	defer func() {
		<-sem // release semaphore
		wg.Done()
	}()

	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {

		// Make the request
		resp, err := httpClient.Get("https://" + a.Endpoint + "/services/files/download/" + nodeId)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d: failed to create request: %w", attempt, err)
			time.Sleep(2 * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("attempt %d: bad status: %s", attempt, resp.Status)
			time.Sleep(2 * time.Second)
			continue
		}

		// Get content length from header (optional)
		contentLengthStr := resp.Header.Get("Content-Length")
		var expectedSize int64 = -1
		if contentLengthStr != "" {
			expectedSize, err = strconv.ParseInt(contentLengthStr, 10, 64)
			if err != nil {
				// If Content-Length is invalid, just ignore size check
				expectedSize = -1
			}
		}

		if expectedSize != fileSize {
			lastErr = fmt.Errorf("attempt %d: download files size mistmatch!", attempt)
			time.Sleep(2 * time.Second)
			continue
		}

		info, err := os.Stat(destPath + "/" + fileName)
		if err == nil {
			if info.Mode().IsRegular() && info.Size() == expectedSize {
				//if file already exist go out!
				exitStatus := "File already exist!"
				resp.Body.Close()
				return &exitStatus, nil
			}
		}

		out, err := os.Create(destPath + "/" + fileName)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d: file create error: %w", attempt, err)
			time.Sleep(2 * time.Second)
			continue
		}

		pw := &ProgressWriter{
			Writer:   out,
			Total:    expectedSize,
			FileName: fileName,
		}
		var written int64
		if expectedSize > 0 {
			written, err = io.Copy(pw, resp.Body)
		} else {
			written, err = io.Copy(out, resp.Body)
		}
		out.Close()
		resp.Body.Close()

		if err != nil {
			lastErr = fmt.Errorf("attempt %d: file write error: %w", attempt, err)
			time.Sleep(2 * time.Second)
			continue
		}

		if expectedSize >= 0 && written != expectedSize {
			lastErr = fmt.Errorf("attempt %d: file size mismatch: expected %d bytes, got %d bytes", attempt, expectedSize, written)
			time.Sleep(2 * time.Second)
			continue
		}

		exitStatus := "File downloaded successfully."
		time.Sleep(1 * time.Second)
		return &exitStatus, nil
	}

	log.Error().Err(lastErr).Str("fileName", fileName).Msg("Download failed after all retries")
	return nil, lastErr
}

// decompressBody returns a ReadCloser that transparently decompresses the
// response body according to the Content-Encoding header (br, gzip, or plain).
// The caller is responsible for closing the returned ReadCloser.
func decompressBody(resp *http.Response) io.ReadCloser {
	switch strings.ToLower(resp.Header.Get("Content-Encoding")) {
	case "br":
		// brotli.NewReader does not return an error at creation time;
		// any invalid-data error surfaces when reading from the returned reader.
		return io.NopCloser(brotli.NewReader(resp.Body))
	case "gzip":
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			// Fall back to raw body if gzip reader cannot be created
			return resp.Body
		}
		return gr
	default:
		return resp.Body
	}
}

func (a *HTTPAuthenticator) UploadFile(
	token string,
	parentId string,
	filePath string,
	newVersion bool,
	overWriteVersion bool,
	nodeId *string,
) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		log.Error().Err(err).Str("filePath", filePath).Msg("Opening file for upload failed")
		return "", err
	}
	defer file.Close()

	mimeType, err := DetectMimeType(filePath)

	if err != nil {
		mimeType = "byte"
	}

	uploadEndpoint := "upload"

	if newVersion {
		uploadEndpoint = "upload-version"
	}

	// Prepare request
	url := "https://" + a.Endpoint + "/services/files/" + uploadEndpoint
	req, err := http.NewRequest("POST", url, file)
	if err != nil {
		log.Error().Err(err).Msg("Building upload request failed")
		return "", err
	}

	filename := ExtractFileName(filePath)

	contentLength, err := GetFileContentLength(filePath)
	if err != nil {
		log.Error().Err(err).Str("filePath", filePath).Msg("Getting file content length failed")
		return "", err
	}

	// Set headers
	//req.Header.Set("AccountId", accountId)
	encodedFilename := base64.StdEncoding.EncodeToString([]byte(filename))

	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Filename", encodedFilename)
	req.Header.Set("ParentId", parentId)
	req.Header.Set("Content-Type", mimeType)
	req.Header.Set("Content-Length", strconv.FormatInt(contentLength, 10))
	req.ContentLength = contentLength

	if newVersion {
		if overWriteVersion {
			req.Header.Set("OverwriteVersion", "true")
		} else {
			req.Header.Set("OverwriteVersion", "false")
		}
		req.Header.Set("NodeId", *nodeId)
	}

	dialer := &net.Dialer{
		Timeout: 5 * time.Second, // Only dial (connection) timeout
	}

	// Optionally, set up an authenticated HTTP client
	httpClient := newAuthenticatedClient(dialer, token)

	// Perform request
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Error().Err(err).Msg("Upload request failed")
		return "", err
	}
	defer resp.Body.Close()

	bodyReader := decompressBody(resp)
	defer bodyReader.Close()
	body, err := io.ReadAll(bodyReader)
	if err != nil {
		return "", fmt.Errorf("failed to read upload response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Error().Int("status", resp.StatusCode).Str("body", string(body)).Msg("Upload failed with non-OK status")
		return "", fmt.Errorf("upload failed: %s", resp.Status)
	}

	log.Debug().Str("body", string(body)).Msg("Upload response body")

	var uploadResp struct {
		NodeId string `json:"nodeId"`
	}
	if err := json.Unmarshal(body, &uploadResp); err != nil {
		return "", fmt.Errorf("failed to parse upload response %q: %w", string(body), err)
	}

	return uploadResp.NodeId, nil
}
