package graphql

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

type API interface {
	GetAllNode(nodeID string)
}

type GraphQLAuthenticator struct {
	Endpoint  string
	AuthToken string
}

// customTransport adds the Cookie header to every request
type customTransport struct {
	base      http.RoundTripper
	authToken string
}

func (t *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cookieValue := fmt.Sprintf("ZM_AUTH_TOKEN=%s", t.authToken)
	req.Header.Set("Cookie", cookieValue)
	return t.base.RoundTrip(req)
}

func (a *GraphQLAuthenticator) GetAllNode(nodeID string) {

	// Optionally, set up an authenticated HTTP client
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &customTransport{
			base:      http.DefaultTransport,
			authToken: a.AuthToken,
		},
	}

	client := NewClient("https://"+a.Endpoint+"/services/files/graphql", httpClient)

	childrenLimit := 25
	sort := "NAME_ASC"         // this should match your GraphQL schema's NodeSort enum
	var pageToken *string      // nil means no page token
	var sharesLimit *int = nil // nil will use the default, or set your value

	var resp *GetChildrenResponse

	// Execute the query
	resp, err := client.GetChildren(
		context.Background(),
		nodeID,
		childrenLimit,
		pageToken,
		sort,
		sharesLimit,
	)

	if err != nil {
		log.Fatalf("GraphQL query failed: %v", err)
	}

	// Print the results
	if resp.Data.GetNode == nil {
		fmt.Println("No node found")
		return
	}
	fmt.Printf("Node: %s, Name: %s\n", resp.Data.GetNode.ID, resp.Data.GetNode.Name)
	if resp.Data.GetNode.Children != nil {
		fmt.Println("Children:")
		for _, child := range resp.Data.GetNode.Children.Nodes {
			fmt.Printf("- Child Node: %s (%s)\n", child.ID, child.Name)
		}
	}
}
