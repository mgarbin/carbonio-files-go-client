package actions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newSelfSignedActionsServer starts a TLS server (self-signed certificate,
// same as httptest.NewTLSServer always produces) that answers every POST
// with body as the GraphQL response payload. It returns the "host:port" to
// plug into MoveNodes/TrashNodes/DeleteNodes's endpoint parameter, which -
// via the graphql.GraphQLAuthenticator each of them builds internally -
// always gets "https://" prepended.
func newSelfSignedActionsServer(t *testing.T, body map[string]any) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

// TestMoveNodes_EmptyArguments locks in that MoveNodes rejects a missing
// destinationId and/or nodesIdList before ever touching the network - the
// error (already logged by MoveNodes itself) must come back regardless of
// which of the two required arguments is blank.
func TestMoveNodes_EmptyArguments(t *testing.T) {
	cases := map[string]struct {
		destinationId string
		nodesIdList   string
	}{
		"empty destinationId": {destinationId: "", nodesIdList: "node-1"},
		"empty nodesIdList":   {destinationId: "dest-1", nodesIdList: ""},
		"both empty":          {destinationId: "", nodesIdList: ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := MoveNodes("unused-endpoint", "tok", tc.destinationId, tc.nodesIdList); err == nil {
				t.Fatalf("MoveNodes(destinationId=%q, nodesIdList=%q) error = nil, want non-nil", tc.destinationId, tc.nodesIdList)
			}
		})
	}
}

// TestTrashNodes_EmptyNodesIdList locks in that TrashNodes rejects a
// missing nodesIdList before ever touching the network.
func TestTrashNodes_EmptyNodesIdList(t *testing.T) {
	if err := TrashNodes("unused-endpoint", "tok", ""); err == nil {
		t.Fatalf("TrashNodes(nodesIdList=\"\") error = nil, want non-nil")
	}
}

// TestDeleteNodes_EmptyNodesIdList locks in that DeleteNodes rejects a
// missing nodesIdList before ever touching the network.
func TestDeleteNodes_EmptyNodesIdList(t *testing.T) {
	if err := DeleteNodes("unused-endpoint", "tok", ""); err == nil {
		t.Fatalf("DeleteNodes(nodesIdList=\"\") error = nil, want non-nil")
	}
}

// TestMoveNodes_AcceptsSelfSignedCertificate confirms MoveNodes correctly
// forwards to graphql.GraphQLAuthenticator.MoveNodes and accepts the
// self-signed certificate every production Carbonio server presents (see
// graphql.newAuthenticatedClient's InsecureSkipVerify wiring).
func TestMoveNodes_AcceptsSelfSignedCertificate(t *testing.T) {
	endpoint := newSelfSignedActionsServer(t, map[string]any{
		"data": map[string]any{
			"moveNodes": []map[string]any{
				{"id": "node-1", "parent": map[string]any{"id": "dest-1"}},
			},
		},
	})

	if err := MoveNodes(endpoint, "tok-123", "dest-1", "node-1"); err != nil {
		t.Fatalf("MoveNodes() error = %v, want nil (self-signed cert must be accepted)", err)
	}
}

// TestTrashNodes_AcceptsSelfSignedCertificate confirms TrashNodes correctly
// forwards to graphql.GraphQLAuthenticator.TrashNodes and accepts the
// self-signed certificate.
func TestTrashNodes_AcceptsSelfSignedCertificate(t *testing.T) {
	endpoint := newSelfSignedActionsServer(t, map[string]any{
		"data": map[string]any{"trashNodes": []string{"node-1"}},
	})

	if err := TrashNodes(endpoint, "tok-123", "node-1"); err != nil {
		t.Fatalf("TrashNodes() error = %v, want nil (self-signed cert must be accepted)", err)
	}
}

// TestDeleteNodes_AcceptsSelfSignedCertificate confirms DeleteNodes
// correctly forwards to graphql.GraphQLAuthenticator.DeleteNodes and
// accepts the self-signed certificate.
func TestDeleteNodes_AcceptsSelfSignedCertificate(t *testing.T) {
	endpoint := newSelfSignedActionsServer(t, map[string]any{
		"data": map[string]any{"deleteNodes": []string{"node-1"}},
	})

	if err := DeleteNodes(endpoint, "tok-123", "node-1"); err != nil {
		t.Fatalf("DeleteNodes() error = %v, want nil (self-signed cert must be accepted)", err)
	}
}

// TestSyncSummary_HasChanges locks in HasChanges's contract: it must report
// true as soon as any one of New/Modified/Deleted holds an entry, and false
// for the zero value as well as for a summary whose slices were
// initialized but never appended to (empty-but-non-nil), since neither
// case produced anything worth a desktop notification.
func TestSyncSummary_HasChanges(t *testing.T) {
	cases := map[string]struct {
		summary SyncSummary
		want    bool
	}{
		"zero value": {
			summary: SyncSummary{},
			want:    false,
		},
		"only New": {
			summary: SyncSummary{New: []SyncChange{{Path: "a.txt"}}},
			want:    true,
		},
		"only Modified": {
			summary: SyncSummary{Modified: []SyncChange{{Path: "b.txt"}}},
			want:    true,
		},
		"only Deleted": {
			summary: SyncSummary{Deleted: []SyncChange{{Path: "c.txt"}}},
			want:    true,
		},
		"all empty but non-nil slices": {
			summary: SyncSummary{
				New:      []SyncChange{},
				Modified: []SyncChange{},
				Deleted:  []SyncChange{},
			},
			want: false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.summary.HasChanges(); got != tc.want {
				t.Fatalf("HasChanges() = %v, want %v", got, tc.want)
			}
		})
	}
}
