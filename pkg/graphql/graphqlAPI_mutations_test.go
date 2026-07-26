package graphql

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Regression test companion to graphqlAPI_test.go: MoveNodes, TrashNodes and
// DeleteNodes build their HTTP client the same way GetAllNode/CreateFolder
// do (via newAuthenticatedClient, see the comment on
// TestGetAllNode_AcceptsSelfSignedCertificate), so they must also accept a
// self-signed certificate and must correctly decode their respective
// mutation responses.
func TestMoveNodes_AcceptsSelfSignedCertificateAndReturnsMovedIDs(t *testing.T) {
	endpoint := newSelfSignedGraphQLServer(t, map[string]any{
		"data": map[string]any{
			"moveNodes": []map[string]any{
				{"id": "node-1", "parent": map[string]any{"id": "target-parent"}},
				{"id": "node-2", "parent": map[string]any{"id": "target-parent"}},
			},
		},
	})

	a := &GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "tok-123"}
	moved, err := a.MoveNodes([]string{"node-1", "node-2"}, "target-parent")
	if err != nil {
		t.Fatalf("MoveNodes() error = %v, want nil (self-signed cert must be accepted)", err)
	}

	want := []string{"node-1", "node-2"}
	if len(moved) != len(want) {
		t.Fatalf("MoveNodes() = %v, want %v", moved, want)
	}
	for i, id := range want {
		if moved[i] != id {
			t.Fatalf("MoveNodes()[%d] = %q, want %q", i, moved[i], id)
		}
	}
}

func TestTrashNodes_AcceptsSelfSignedCertificateAndReturnsTrashedIDs(t *testing.T) {
	endpoint := newSelfSignedGraphQLServer(t, map[string]any{
		"data": map[string]any{
			"trashNodes": []string{"node-1", "node-2"},
		},
	})

	a := &GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "tok-123"}
	trashed, err := a.TrashNodes([]string{"node-1", "node-2"})
	if err != nil {
		t.Fatalf("TrashNodes() error = %v, want nil (self-signed cert must be accepted)", err)
	}

	want := []string{"node-1", "node-2"}
	if len(trashed) != len(want) {
		t.Fatalf("TrashNodes() = %v, want %v", trashed, want)
	}
	for i, id := range want {
		if trashed[i] != id {
			t.Fatalf("TrashNodes()[%d] = %q, want %q", i, trashed[i], id)
		}
	}
}

func TestDeleteNodes_AcceptsSelfSignedCertificateAndReturnsDeletedIDs(t *testing.T) {
	endpoint := newSelfSignedGraphQLServer(t, map[string]any{
		"data": map[string]any{
			"deleteNodes": []string{"node-1", "node-2"},
		},
	})

	a := &GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "tok-123"}
	deleted, err := a.DeleteNodes([]string{"node-1", "node-2"})
	if err != nil {
		t.Fatalf("DeleteNodes() error = %v, want nil (self-signed cert must be accepted)", err)
	}

	want := []string{"node-1", "node-2"}
	if len(deleted) != len(want) {
		t.Fatalf("DeleteNodes() = %v, want %v", deleted, want)
	}
	for i, id := range want {
		if deleted[i] != id {
			t.Fatalf("DeleteNodes()[%d] = %q, want %q", i, deleted[i], id)
		}
	}
}

// newFailingGraphQLServer starts a TLS server (self-signed certificate)
// that answers every POST with the given HTTP status and a malformed
// (non-JSON) body, simulating a server-side failure.
func newFailingGraphQLServer(t *testing.T, status int) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

// A server error (HTTP 500 with a malformed body) must surface as a
// non-nil error from every mutation method, not be silently swallowed or
// cause a panic while decoding the response.
func TestMutations_ServerErrorReturnsError(t *testing.T) {
	endpoint := newFailingGraphQLServer(t, http.StatusInternalServerError)
	a := &GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "tok-123"}

	if _, err := a.MoveNodes([]string{"node-1"}, "target-parent"); err == nil {
		t.Fatalf("MoveNodes() error = nil, want non-nil on server error")
	}
	if _, err := a.TrashNodes([]string{"node-1"}); err == nil {
		t.Fatalf("TrashNodes() error = nil, want non-nil on server error")
	}
	if _, err := a.DeleteNodes([]string{"node-1"}); err == nil {
		t.Fatalf("DeleteNodes() error = nil, want non-nil on server error")
	}
}
