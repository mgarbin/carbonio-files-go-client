package graphql

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"

	genqlient "github.com/Khan/genqlient/graphql"
	"github.com/rs/zerolog/log"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

type API interface {
	GetAllNode(nodeID string)
}

// GraphQLAuthenticator issues GraphQL requests against Endpoint,
// authenticating with AuthToken (the ZM_AUTH_TOKEN cookie). ZM_AUTH_TOKEN
// normally expires after a fixed window (commonly 8 hours, but server
// configurable) - once it does, the server rejects every request with a
// bare HTTP 401 ("Unable to find requested user"). If TokenRefresher is
// set, every method here reacts to that 401 by calling it to obtain a
// fresh token, updating AuthToken, and retrying the failed request exactly
// once - so a long-lived caller (e.g. the desktop GUI's background sync
// loop) survives token expiry without user intervention. TokenRefresher is
// typically *carbonio.Session's Reauthenticate method. A nil TokenRefresher
// (the zero value) disables this: a 401 is simply returned as an error,
// matching the pre-existing behavior.
type GraphQLAuthenticator struct {
	Endpoint       string
	AuthToken      string
	TokenRefresher func() (string, error)
}

// customTransport adds the Cookie header to every request. TLS/dialer
// settings live on base (see newAuthenticatedClient), never on this
// struct - a field here would be silently ignored, since RoundTrip only
// ever delegates to base.
type customTransport struct {
	base      http.RoundTripper
	authToken string
}

func (t *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cookieValue := fmt.Sprintf("ZM_AUTH_TOKEN=%s", t.authToken)
	req.Header.Set("Cookie", cookieValue)
	return t.base.RoundTrip(req)
}

// newAuthenticatedClient builds an http.Client that authenticates every
// request via the ZM_AUTH_TOKEN cookie and talks to the Carbonio server
// over TLS without verifying its certificate (Carbonio servers commonly
// use a self-signed certificate; see docs/notes.md).
func newAuthenticatedClient(authToken string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &customTransport{
			base:      &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
			authToken: authToken,
		},
	}
}

// isUnauthorized reports whether err is the genqlient HTTPError carbonio-auth
// returns for a rejected ZM_AUTH_TOKEN: AuthorizedApiHandler maps every
// reason a token stops being usable (expired, deregistered, malformed) to a
// bare HTTP 401, so a 401 here is the correct, and only, signal to
// re-authenticate.
func isUnauthorized(err error) bool {
	var httpErr *genqlient.HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnauthorized
}

// executeWithReauth runs op, which must perform exactly one GraphQL request
// using the *http.Client it is given (built from a.AuthToken). If op fails
// with isUnauthorized and a.TokenRefresher is set, executeWithReauth calls
// TokenRefresher to obtain a fresh ZM_AUTH_TOKEN, stores it on a.AuthToken,
// and retries op exactly once with a client built from the new token. Any
// other error, or a refresh failure, is returned as-is from the failing
// attempt - op is never retried more than once.
func (a *GraphQLAuthenticator) executeWithReauth(op func(httpClient *http.Client) error) error {
	err := op(newAuthenticatedClient(a.AuthToken))
	if err == nil || a.TokenRefresher == nil || !isUnauthorized(err) {
		return err
	}

	log.Warn().Msg("GraphQL request unauthorized, ZM_AUTH_TOKEN likely expired - requesting a new one")
	newToken, refreshErr := a.TokenRefresher()
	if refreshErr != nil {
		log.Error().Err(refreshErr).Msg("Failed to refresh ZM_AUTH_TOKEN after 401")
		return err
	}
	a.AuthToken = newToken
	return op(newAuthenticatedClient(a.AuthToken))
}

// maxTransientAttempts and transientBackoffBase bound withTransientRetry: up
// to 3 attempts total, sleeping 150ms then 300ms between them (~450ms worst
// case) before giving up on a run of transient failures.
const (
	maxTransientAttempts = 3
	transientBackoffBase = 150 * time.Millisecond
)

// isTransient reports whether err is worth retrying: a network-level
// failure (dial/TLS/timeout/connection reset - MakeRequest returns these
// straight from http.Client.Do, unwrapped, see genqlient's client.go) or a
// 5xx/429 HTTPError. A 401 never reaches here as a final error - it's
// already fully handled by executeWithReauth before returning. Any other
// HTTPError status (4xx) or a well-formed response carrying GraphQL
// execution errors (gqlerror.List, e.g. a bad query or a permission
// error) is a permanent problem: retrying the identical request would
// just reproduce it.
func isTransient(err error) bool {
	var httpErr *genqlient.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= http.StatusInternalServerError || httpErr.StatusCode == http.StatusTooManyRequests
	}
	var gqlErrs gqlerror.List
	if errors.As(err, &gqlErrs) {
		return false
	}
	return true
}

// withTransientRetry runs fn - a single GraphQL round trip, typically
// wrapping executeWithReauth - up to maxTransientAttempts times with a
// doubling backoff, but only while isTransient keeps returning true.
// GetAllNode is the only caller: it is read-only, so retrying it is always
// safe. Mutations (CreateFolder, MoveNodes, TrashNodes, DeleteNodes) are
// deliberately NOT wrapped in this - see findExistingRemoteChild's doc
// comment in pkg/actions/actions.go: a lost response after a mutation that
// actually succeeded server-side would turn a blind retry into a
// duplicate/second action.
func withTransientRetry(fn func() error) error {
	var err error
	for attempt := range maxTransientAttempts {
		if err = fn(); err == nil || !isTransient(err) {
			return err
		}
		if attempt < maxTransientAttempts-1 {
			backoff := transientBackoffBase << attempt
			log.Warn().Err(err).Int("attempt", attempt+1).Dur("backoff", backoff).Msg("Transient GraphQL error, retrying")
			time.Sleep(backoff)
		}
	}
	return err
}

func (a *GraphQLAuthenticator) GetAllNode(nodeID string, sort string, pageToken *string, sharesLimit *int) ([]*Node, error) {
	//hard coded for now
	childrenLimit := 25

	var resp *GetChildrenResponse
	err := withTransientRetry(func() error {
		return a.executeWithReauth(func(httpClient *http.Client) error {
			client := NewClient("https://"+a.Endpoint+"/services/files/graphql", httpClient)
			var queryErr error
			resp, queryErr = client.GetChildren(
				context.Background(),
				nodeID,
				childrenLimit,
				pageToken,
				sort,
				sharesLimit,
			)
			return queryErr
		})
	})

	if err != nil {
		log.Error().Err(err).Msg("GraphQL query failed")
		return nil, err
	}

	if resp.GetNode == nil {
		return nil, nil
	}

	var children []*Node

	if resp.GetNode.Children != nil {

		if resp.GetNode.Children.PageToken != nil {
			tokenChild, tokenErr := a.GetAllNode(nodeID, sort, resp.GetNode.Children.PageToken, nil)
			if tokenErr != nil {
				return nil, tokenErr
			}
			children = append(resp.GetNode.Children.Nodes, tokenChild...)
			return children, nil
		}

		return resp.GetNode.Children.Nodes, nil
	}

	return nil, nil
}

func (a *GraphQLAuthenticator) CreateFolder(parentId string, folderName string) (*Folder, error) {
	//hard coded for now
	sharesLimit := 6

	var resp *CreateFolderResponse
	err := a.executeWithReauth(func(httpClient *http.Client) error {
		client := NewClient("https://"+a.Endpoint+"/services/files/graphql", httpClient)
		var queryErr error
		resp, queryErr = client.CreateFolder(
			context.Background(),
			parentId,
			folderName,
			&sharesLimit,
		)
		return queryErr
	})

	if err != nil {
		log.Error().Err(err).Msg("GraphQL query failed")
		return nil, err
	}

	// Print the results
	if resp.CreateFolder.ID == "" {
		return nil, nil
	}

	return resp.CreateFolder, nil
}

func (a *GraphQLAuthenticator) MoveNodes(nodeIds []string, targetParentId string) ([]string, error) {
	var resp *MoveNodesResponse
	err := a.executeWithReauth(func(httpClient *http.Client) error {
		client := NewClient("https://"+a.Endpoint+"/services/files/graphql", httpClient)
		var queryErr error
		resp, queryErr = client.MoveNodes(
			context.Background(),
			nodeIds,
			targetParentId,
		)
		return queryErr
	})

	if err != nil {
		log.Error().Err(err).Msg("GraphQL query failed")
		return nil, err
	}

	var movedNodes []string
	for _, moveNode := range resp.MoveNodes {
		movedNodes = append(movedNodes, moveNode.ID)
	}

	return movedNodes, nil
}

func (a *GraphQLAuthenticator) TrashNodes(nodeIds []string) ([]string, error) {
	var resp *TrashNodesResponse
	err := a.executeWithReauth(func(httpClient *http.Client) error {
		client := NewClient("https://"+a.Endpoint+"/services/files/graphql", httpClient)
		var queryErr error
		resp, queryErr = client.TrashNodes(
			context.Background(),
			nodeIds,
		)
		return queryErr
	})

	if err != nil {
		log.Error().Err(err).Msg("GraphQL query failed")
		return nil, err
	}

	var trashedNodes []string
	for _, trashNode := range resp.TrashNodes {
		trashedNodes = append(trashedNodes, trashNode)
	}

	return trashedNodes, nil
}

func (a *GraphQLAuthenticator) DeleteNodes(nodeIds []string) ([]string, error) {
	var resp *DeleteNodesResponse
	err := a.executeWithReauth(func(httpClient *http.Client) error {
		client := NewClient("https://"+a.Endpoint+"/services/files/graphql", httpClient)
		var queryErr error
		resp, queryErr = client.DeleteNodes(
			context.Background(),
			nodeIds,
		)
		return queryErr
	})

	if err != nil {
		log.Error().Err(err).Msg("GraphQL query failed")
		return nil, err
	}

	var deletedNodes []string
	for _, deleteNode := range resp.DeleteNodes {
		deletedNodes = append(deletedNodes, deleteNode)
	}

	return deletedNodes, nil
}
