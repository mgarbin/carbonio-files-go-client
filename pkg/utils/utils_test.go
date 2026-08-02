package utils

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"carbonio-files-go-client/pkg/graphql"
	"carbonio-files-go-client/pkg/localfs"
)

// TestEpochToTime locks in the four epoch-precision buckets EpochToTime
// picks between (seconds / milliseconds / microseconds / nanoseconds),
// using inputs solidly inside each bucket relative to the thresholds in
// the source (1e12, 1e15, 1e18). time.Time.Equal is used for comparison
// since Unix/UnixMilli/UnixMicro/Unix(0,ns) construct time.Time values
// that need not be byte-identical to compare equal as instants.
func TestEpochToTime(t *testing.T) {
	tests := []struct {
		name string
		ts   int64
		want time.Time
	}{
		{"seconds", 1_700_000_000, time.Unix(1_700_000_000, 0)},
		{"milliseconds", 1_700_000_000_000, time.UnixMilli(1_700_000_000_000)},
		{"microseconds", 1_700_000_000_000_000, time.UnixMicro(1_700_000_000_000_000)},
		{"nanoseconds", 1_700_000_000_000_000_000, time.Unix(0, 1_700_000_000_000_000_000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EpochToTime(tt.ts)
			if !got.Equal(tt.want) {
				t.Fatalf("EpochToTime(%d) = %v, want %v", tt.ts, got, tt.want)
			}
		})
	}
}

// TestCreateLocalFolder covers the three observable outcomes of
// CreateLocalFolder: it creates a brand-new directory, it is idempotent
// (returns nil, not an os.IsExist error, when the directory already
// exists - this is the whole reason the function exists instead of a bare
// os.Mkdir call), and it still surfaces any other underlying error, such
// as a missing parent directory.
func TestCreateLocalFolder(t *testing.T) {
	t.Run("creates new directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "newfolder")

		if err := CreateLocalFolder(dir); err != nil {
			t.Fatalf("CreateLocalFolder(%q) error = %v, want nil", dir, err)
		}

		info, statErr := os.Stat(dir)
		if statErr != nil {
			t.Fatalf("os.Stat(%q) error = %v, want nil (folder should have been created)", dir, statErr)
		}
		if !info.IsDir() {
			t.Fatalf("%q exists but is not a directory", dir)
		}
	})

	t.Run("idempotent when directory already exists", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "existing")
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatalf("setup os.Mkdir(%q) error = %v", dir, err)
		}

		if err := CreateLocalFolder(dir); err != nil {
			t.Fatalf("CreateLocalFolder(%q) on already-existing dir error = %v, want nil", dir, err)
		}
	})

	t.Run("propagates error when parent directory is missing", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "missing-parent", "child")

		if err := CreateLocalFolder(dir); err == nil {
			t.Fatalf("CreateLocalFolder(%q) error = nil, want non-nil (parent directory does not exist)", dir)
		}
	})
}

// newChildrenServer starts a self-signed TLS server (httptest.NewTLSServer,
// matching the pattern in pkg/graphql/graphqlAPI_test.go - production code
// always talks HTTPS with InsecureSkipVerify) that plays the role of the
// Carbonio getChildren GraphQL endpoint. It inspects the "node_id" GraphQL
// variable of every incoming request and answers with the *graphql.Node
// registered for that id, so recursive calls (one HTTP round-trip per
// folder) can be driven to different, non-looping responses. It returns
// the "host:port" to plug into GraphQLAuthenticator.Endpoint, which always
// prepends "https://".
func newChildrenServer(t *testing.T, byNodeID map[string]*graphql.Node) string {
	t.Helper()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		nodeID, _ := body.Variables["node_id"].(string)

		w.Header().Set("Content-Type", "application/json")
		node := byNodeID[nodeID]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"getNode": node},
		})
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

func strPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

func int64Ptr(i int64) *int64 {
	return &i
}

func f64Ptr(f float64) *float64 {
	return &f
}

// TestRecursiveListNodeItems_BuildsFlatPathMap drives RecursiveListNodeItems
// against a self-signed TLS server that mirrors the getChildren response
// shape from pkg/graphql/getChildren.go across two node ids: the root
// (mixing a FOLDER and a FILE child) and the folder's own id (recursed into
// automatically), asserting the returned map has the expected
// folderPath-prefixed keys, IsDirectory-equivalent (IsFile) flag and the
// rest of the ItemInfo fields (permissions, digest, size, version, mime
// type, modify timestamp) copied over correctly for both files and
// folders.
func TestRecursiveListNodeItems_BuildsFlatPathMap(t *testing.T) {
	root := &graphql.Node{
		ID:   "root-id",
		Name: "Root",
		Type: "FOLDER",
		Children: &graphql.Children{
			Nodes: []*graphql.Node{
				{
					ID:          "folder-1",
					Name:        "sub",
					Type:        "FOLDER",
					Permissions: &graphql.Permissions{CanWriteFile: true, CanAddVersion: false, CanDelete: true},
				},
				{
					ID:          "file-1",
					Name:        "file1",
					Type:        "FILE",
					Extension:   strPtr("txt"),
					Digest:      strPtr("abc123"),
					Size:        f64Ptr(42),
					UpdatedAt:   int64Ptr(1_700_000_000),
					Version:     intPtr(3),
					MimeType:    strPtr("text/plain"),
					Permissions: &graphql.Permissions{CanWriteFile: false, CanAddVersion: true, CanDelete: false},
				},
			},
		},
	}
	sub := &graphql.Node{
		ID:   "folder-1",
		Name: "sub",
		Type: "FOLDER",
		Children: &graphql.Children{
			Nodes: []*graphql.Node{
				{
					ID:          "file-2",
					Name:        "nested",
					Type:        "FILE",
					Extension:   strPtr("md"),
					Digest:      strPtr("def456"),
					Size:        f64Ptr(7.5),
					UpdatedAt:   int64Ptr(1_700_000_111),
					Version:     intPtr(1),
					MimeType:    strPtr("text/markdown"),
					Permissions: &graphql.Permissions{CanWriteFile: true, CanAddVersion: true, CanDelete: true},
				},
			},
		},
	}

	endpoint := newChildrenServer(t, map[string]*graphql.Node{
		"root-id":  root,
		"folder-1": sub,
	})
	auth := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "tok-123"}

	got, failedPaths, err := RecursiveListNodeItems(auth, "root-id", "")
	if err != nil {
		t.Fatalf("RecursiveListNodeItems() error = %v, want nil", err)
	}
	if len(failedPaths) != 0 {
		t.Fatalf("RecursiveListNodeItems() failedPaths = %v, want none", failedPaths)
	}

	want := map[string]localfs.ItemInfo{
		"sub": {
			IsFile: false, NodeId: "folder-1",
			CanWriteFile: true, CanAddVersion: false, CanDelete: true,
		},
		"file1.txt": {
			IsFile: true, NodeId: "file-1",
			Digest: "abc123", Size: 42, ModifyTimestamp: 1_700_000_000, FileVersion: 3, MimeType: "text/plain",
			CanWriteFile: false, CanAddVersion: true, CanDelete: false,
		},
		"sub/nested.md": {
			IsFile: true, NodeId: "file-2",
			Digest: "def456", Size: 7.5, ModifyTimestamp: 1_700_000_111, FileVersion: 1, MimeType: "text/markdown",
			CanWriteFile: true, CanAddVersion: true, CanDelete: true,
		},
	}

	if len(got) != len(want) {
		t.Fatalf("RecursiveListNodeItems() = %d entries %+v, want %d entries %+v", len(got), got, len(want), want)
	}
	for path, wantItem := range want {
		gotItem, ok := got[path]
		if !ok {
			t.Fatalf("RecursiveListNodeItems() missing key %q, got %+v", path, got)
		}
		if gotItem != wantItem {
			t.Fatalf("RecursiveListNodeItems()[%q] = %+v, want %+v", path, gotItem, wantItem)
		}
	}
}

// TestRecursiveListNodeItems_PropagatesGetAllNodeError checks that when the
// underlying GraphQLAuthenticator.GetAllNode call fails,
// RecursiveListNodeItems returns that error (and a nil map) instead of
// masking it. The server is a plain (non-TLS) httptest server; production
// code always dials "https://"+Endpoint, so the TLS handshake against a
// non-TLS listener fails before any GraphQL request/response is exchanged.
func TestRecursiveListNodeItems_PropagatesGetAllNodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	endpoint := strings.TrimPrefix(srv.URL, "http://")
	auth := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "tok-123"}

	items, failedPaths, err := RecursiveListNodeItems(auth, "root-id", "")
	if err == nil {
		t.Fatalf("RecursiveListNodeItems() error = nil, want non-nil (TLS handshake against a non-TLS endpoint must fail)")
	}
	if items != nil {
		t.Fatalf("RecursiveListNodeItems() items = %+v, want nil", items)
	}
	if failedPaths != nil {
		t.Fatalf("RecursiveListNodeItems() failedPaths = %+v, want nil", failedPaths)
	}
}

// TestRecursiveListNodeItems_SkipsFailedSubtreeButKeepsSiblings covers the
// partial-failure containment fix: when one subfolder's getChildren calls
// keep failing (GetAllNode's own transient retries exhausted), that
// subtree alone is skipped - its own folder entry is still recorded, and
// its path lands in failedPaths - while every sibling folder's
// already-fetched items are kept, instead of the whole walk being
// discarded.
func TestRecursiveListNodeItems_SkipsFailedSubtreeButKeepsSiblings(t *testing.T) {
	root := &graphql.Node{
		ID:   "root-id",
		Name: "Root",
		Type: "FOLDER",
		Children: &graphql.Children{
			Nodes: []*graphql.Node{
				{ID: "good-id", Name: "good", Type: "FOLDER"},
				{ID: "bad-id", Name: "bad", Type: "FOLDER"},
			},
		},
	}
	good := &graphql.Node{
		ID:   "good-id",
		Name: "good",
		Type: "FOLDER",
		Children: &graphql.Children{
			Nodes: []*graphql.Node{
				{ID: "file-1", Name: "ok", Type: "FILE", Extension: strPtr("txt")},
			},
		},
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		nodeID, _ := body.Variables["node_id"].(string)
		if nodeID == "bad-id" {
			// Every attempt fails, including GetAllNode's own transient
			// retries - simulates a subtree the server keeps erroring on.
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		var node *graphql.Node
		switch nodeID {
		case "root-id":
			node = root
		case "good-id":
			node = good
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"getNode": node},
		})
	}))
	t.Cleanup(srv.Close)

	auth := &graphql.GraphQLAuthenticator{Endpoint: strings.TrimPrefix(srv.URL, "https://"), AuthToken: "tok-123"}

	got, failedPaths, err := RecursiveListNodeItems(auth, "root-id", "")
	if err != nil {
		t.Fatalf("RecursiveListNodeItems() error = %v, want nil (only a root-level fetch failure is fatal)", err)
	}
	if want := []string{"bad"}; len(failedPaths) != 1 || failedPaths[0] != want[0] {
		t.Fatalf("RecursiveListNodeItems() failedPaths = %v, want %v", failedPaths, want)
	}
	for _, wantPath := range []string{"good", "good/ok.txt", "bad"} {
		if _, ok := got[wantPath]; !ok {
			t.Fatalf("RecursiveListNodeItems() missing key %q, got %+v", wantPath, got)
		}
	}
}
