package graphql

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newSelfSignedGraphQLServer starts a TLS server (self-signed certificate,
// same as httptest.NewTLSServer always produces) that answers every POST
// with body as the GraphQL response payload. It returns the "host:port" to
// plug into GraphQLAuthenticator.Endpoint (which always prepends "https://").
func newSelfSignedGraphQLServer(t *testing.T, body map[string]any) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

// Regression test for the bug behind the dashboard's "Errori di
// sincronizzazione" / "tls: failed to verify certificate: x509:
// certificate signed by unknown authority" report: customTransport used to
// carry a TLSClientConfig field that RoundTrip never read (it always
// delegated to base, which was hardcoded to http.DefaultTransport), so the
// intended InsecureSkipVerify was silently discarded and every GraphQL
// call rejected the Carbonio server's self-signed certificate. Every
// GraphQLAuthenticator method now builds its client via
// newAuthenticatedClient, which wires InsecureSkipVerify into the real
// base transport - this must keep working against a self-signed server.
func TestGetAllNode_AcceptsSelfSignedCertificate(t *testing.T) {
	endpoint := newSelfSignedGraphQLServer(t, map[string]any{
		"data": map[string]any{"getNode": nil},
	})

	a := &GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "tok-123"}
	nodes, err := a.GetAllNode("some-id", "NAME_ASC", nil, nil)
	if err != nil {
		t.Fatalf("GetAllNode() error = %v, want nil (self-signed cert must be accepted)", err)
	}
	if nodes != nil {
		t.Fatalf("GetAllNode() nodes = %v, want nil", nodes)
	}
}

func TestCreateFolder_AcceptsSelfSignedCertificate(t *testing.T) {
	endpoint := newSelfSignedGraphQLServer(t, map[string]any{
		"data": map[string]any{
			"createFolder": map[string]any{"id": "folder-1", "name": "New folder"},
		},
	})

	a := &GraphQLAuthenticator{Endpoint: endpoint, AuthToken: "tok-123"}
	folder, err := a.CreateFolder("parent-1", "New folder")
	if err != nil {
		t.Fatalf("CreateFolder() error = %v, want nil (self-signed cert must be accepted)", err)
	}
	if folder == nil || folder.ID != "folder-1" {
		t.Fatalf("CreateFolder() folder = %+v, want ID=folder-1", folder)
	}
}
