package graphql

import (
	"context"
	"crypto/tls"
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
	base            http.RoundTripper
	TLSClientConfig *tls.Config
	authToken       string
}

func (t *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cookieValue := fmt.Sprintf("ZM_AUTH_TOKEN=%s", t.authToken)
	req.Header.Set("Cookie", cookieValue)
	return t.base.RoundTrip(req)
}

func (a *GraphQLAuthenticator) GetAllNode(nodeID string, sort string, pageToken *string, sharesLimit *int) ([]*Node, error) {
	// Optionally, set up an authenticated HTTP client
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &customTransport{
			base:            http.DefaultTransport,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			authToken:       a.AuthToken,
		},
	}

	client := NewClient("https://"+a.Endpoint+"/services/files/graphql", httpClient)

	//hard coded for now
	childrenLimit := 25

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
		return nil, err
	}

	// Print the results
	if resp.GetNode == nil {
		//fmt.Println("No node found")
		return nil, nil
	}

	var children []*Node

	//fmt.Printf("Node: %s, Name: %s\n", resp.GetNode.ID, resp.GetNode.Name)
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
		/*fmt.Println("Children:")
		for _, child := range resp.GetNode.Children.Nodes {
			fmt.Printf("- Child Node: %s (%s)\n", child.ID, child.Name)
		}*/
	}

	return nil, nil
}

func (a *GraphQLAuthenticator) CreateFolder(parentId string, folderName string) (*Folder, error) {
	// Optionally, set up an authenticated HTTP client
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &customTransport{
			base:            http.DefaultTransport,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			authToken:       a.AuthToken,
		},
	}

	client := NewClient("https://"+a.Endpoint+"/services/files/graphql", httpClient)

	//hard coded for now
	sharesLimit := 6

	// Execute the query
	resp, err := client.CreateFolder(
		context.Background(),
		parentId,
		folderName,
		&sharesLimit,
	)

	if err != nil {
		log.Fatalf("GraphQL query failed: %v", err)
		return nil, err
	}

	// Print the results
	if resp.CreateFolder.ID == "" {
		return nil, nil
	}

	return resp.CreateFolder, nil
}
