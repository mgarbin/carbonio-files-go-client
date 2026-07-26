package carbonio

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Regression test for the bug behind the dashboard's "Errori di
// sincronizzazione" / "tls: failed to verify certificate: x509:
// certificate signed by unknown authority" report: customTransport used to
// carry TLS/dialer fields (TLSClientConfig included) that RoundTrip never
// read - it always delegated to base, which was hardcoded to
// http.DefaultTransport, so the intended InsecureSkipVerify was silently
// discarded. DownloadFile and UploadFile now build their client via
// newAuthenticatedClient, which wires InsecureSkipVerify into the real
// base transport - this must keep working against a self-signed server,
// exactly like a Carbonio instance's default certificate.

func TestDownloadFile_AcceptsSelfSignedCertificate(t *testing.T) {
	const content = "hello carbonio"

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)

	destDir := t.TempDir()
	a := &HTTPAuthenticator{Endpoint: strings.TrimPrefix(srv.URL, "https://")}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	wg.Add(1)
	sem <- struct{}{}

	status, err := a.DownloadFile("tok-123", "node-1", destDir, "hello.txt", int64(len(content)), 1, &wg, sem)
	if err != nil {
		t.Fatalf("DownloadFile() error = %v, want nil (self-signed cert must be accepted)", err)
	}
	if status == nil {
		t.Fatal("DownloadFile() status = nil, want non-nil")
	}

	got, err := os.ReadFile(destDir + "/hello.txt")
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if string(got) != content {
		t.Fatalf("downloaded content = %q, want %q", got, content)
	}
}

func TestUploadFile_AcceptsSelfSignedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"nodeId":"node-42"}`))
	}))
	t.Cleanup(srv.Close)

	srcPath := t.TempDir() + "/upload.txt"
	if err := os.WriteFile(srcPath, []byte("payload"), 0o644); err != nil {
		t.Fatalf("writing source file: %v", err)
	}

	a := &HTTPAuthenticator{Endpoint: strings.TrimPrefix(srv.URL, "https://")}
	nodeID, err := a.UploadFile("tok-123", "parent-1", srcPath, false, false, nil)
	if err != nil {
		t.Fatalf("UploadFile() error = %v, want nil (self-signed cert must be accepted)", err)
	}
	if nodeID != "node-42" {
		t.Fatalf("UploadFile() nodeID = %q, want %q", nodeID, "node-42")
	}
}
