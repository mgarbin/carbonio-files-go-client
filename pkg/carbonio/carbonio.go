package carbonio

import (
	"bytes"
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
	"sync"
	"time"
)

type Authenticator interface {
	// Authenticate performs an HTTP POST with email and password,
	// returns the value of the ZM_AUTH_TOKEN cookie if authentication succeeds.
	CarbonioZxAuth(email, password string) (string, error)
	DownloadFile(token, nodeId, destPath string, fileSize int64, maxRetries int) error
	UploadFile(token, parentId, filePath string, newVersion, overWriteVersion bool, nodeId *string) (string, error)
}

type HTTPAuthenticator struct {
	Endpoint string
}

// customTransport adds the Cookie header to every request
type customTransport struct {
	base                  http.RoundTripper
	DialContext           *net.Dialer
	TLSClientConfig       *tls.Config
	DisableKeepAlives     bool
	MaxIdleConns          int
	IdleConnTimeout       int
	ExpectContinueTimeout time.Duration
	authToken             string
}

// ProgressWriter wraps an io.Writer and displays progress.
type ProgressWriter struct {
	Writer      io.Writer
	Total       int64 // expected size
	Downloaded  int64 // bytes written
	LastPrinted int64
	FileName    string
}

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
		return nil, errors.New("invalid email address format")
	}

	// Create payload to inject to zx auth endpoint
	payload := map[string]string{
		"auth_method": "password",
		"user":        email,
		"password":    password,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	// Make the request
	req, err := http.NewRequest("POST", "https://"+a.Endpoint+"/zx/auth/v2/login", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Create HTTP client with SSL verification disabled
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	// Wait for response
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	// Read cookies
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "ZM_AUTH_TOKEN" {
			return &cookie.Value, nil
		}
	}

	return nil, errors.New("ZM_AUTH_TOKEN cookie not found")
}

func (a *HTTPAuthenticator) DownloadFile(token, nodeId, destPath, fileName string, fileSize int64, maxRetries int, wg *sync.WaitGroup, sem chan struct{}) (*string, error) {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second, // Only dial (connection) timeout
	}

	skipInsecure := &tls.Config{InsecureSkipVerify: true}

	// Optionally, set up an authenticated HTTP client
	httpClient := &http.Client{
		Transport: &customTransport{
			DialContext:           dialer,
			TLSClientConfig:       skipInsecure,
			DisableKeepAlives:     false,
			MaxIdleConns:          0,
			IdleConnTimeout:       0,
			ExpectContinueTimeout: 1 * time.Second,
			base:                  http.DefaultTransport,
			authToken:             token,
		},
	}

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

	fmt.Println("Error creating file: %s \n", lastErr)
	return nil, lastErr
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
		fmt.Println("Error: %s \n", err)
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
		fmt.Println("Error: %s \n", err)
		return "", err
	}

	filename := ExtractFileName(filePath)

	contentLength, err := GetFileContentLength(filePath)
	if err != nil {
		fmt.Println("Error: %s \n", err)
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

	skipInsecure := &tls.Config{InsecureSkipVerify: true}

	// Optionally, set up an authenticated HTTP client
	httpClient := &http.Client{
		Transport: &customTransport{
			DialContext:           dialer,
			TLSClientConfig:       skipInsecure,
			DisableKeepAlives:     false,
			MaxIdleConns:          0,
			IdleConnTimeout:       0,
			ExpectContinueTimeout: 1 * time.Second,
			base:                  http.DefaultTransport,
			authToken:             token,
		},
	}

	// Perform request
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Println("Error: %s \n", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read upload response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Println("Error: %s \n", string(body))
		return "", fmt.Errorf("upload failed: %s", resp.Status)
	}

	fmt.Println("Response:", string(body))

	var uploadResp struct {
		NodeId string `json:"nodeId"`
	}
	if err := json.Unmarshal(body, &uploadResp); err != nil {
		return "", fmt.Errorf("failed to parse upload response %q: %w", string(body), err)
	}

	return uploadResp.NodeId, nil
}
