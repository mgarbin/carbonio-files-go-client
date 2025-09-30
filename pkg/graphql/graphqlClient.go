package graphql

import (
	"context"
	"net/http"

	"github.com/Khan/genqlient/graphql"
)

// Client wraps the genqlient GraphQL client
type Client struct {
	client graphql.Client
}

// NewClient creates a new Client with the given endpoint and http.Client
func NewClient(endpoint string, httpClient *http.Client) *Client {
	return &Client{
		client: graphql.NewClient(endpoint, httpClient),
	}
}

// GetChildren executes the getChildren GraphQL query.
// Variables:
// - nodeID: the node id to query children for
// - childrenLimit: limit for children nodes
// - pageToken: optional page token
// - sort: node sort (should match the enum from schema)
// - sharesLimit: optional limit for shares
//
// The response is unmarshaled into the provided resp pointer, which should be of type *GetChildrenResponse.
func (c *Client) GetChildren(
	ctx context.Context,
	nodeID string,
	childrenLimit int,
	pageToken *string,
	sort string,
	sharesLimit *int,
) (_data *GetChildrenResponse, err error) {
	// This is the GraphQL query as a string
	const query = `
	query getChildren($node_id: ID!, $children_limit: Int!, $page_token: String, $sort: NodeSort!, $shares_limit: Int = 1) {
	  getNode(node_id: $node_id) {
		...Parent
		... on Folder {
		  children(limit: $children_limit, page_token: $page_token, sort: $sort) {
			nodes {
			  ...Child
			  __typename
			}
			page_token
			__typename
		  }
		  __typename
		}
		__typename
	  }
	}

	fragment Child on Node {
	  ...BaseNode
	  owner {
		id
		full_name
		email
		__typename
	  }
	  updated_at
	  last_editor {
		id
		full_name
		email
		__typename
	  }
	  shares(limit: $shares_limit) {
		...Share
		__typename
	  }
	  __typename
	}

	fragment BaseNode on Node {
	  id
	  name
	  type
	  ...Permissions
	  ... on File {
		size
		mime_type
		extension
		version
		digest
		__typename
	  }
	  flagged
	  rootId
	  __typename
	}

	fragment Permissions on Node {
	  permissions {
		can_read
		can_write_file
		can_write_folder
		can_delete
		can_add_version
		can_read_link
		can_change_link
		can_share
		can_read_share
		can_change_share
		__typename
	  }
	  __typename
	}

	fragment Share on Share {
	  permission
	  share_target {
		... on User {
		  email
		  full_name
		  id
		  __typename
		}
		... on DistributionList {
		  id
		  name
		  __typename
		}
		__typename
	  }
	  created_at
	  __typename
	}

	fragment Parent on Node {
	  id
	  name
	  type
	  owner {
		id
		full_name
		email
		__typename
	  }
	  ...Permissions
	  __typename
	}
	`

	vars := map[string]interface{}{
		"node_id":        nodeID,
		"children_limit": childrenLimit,
		"page_token":     pageToken,
		"sort":           sort,
	}
	if sharesLimit != nil {
		vars["shares_limit"] = *sharesLimit
	}

	_data = &GetChildrenResponse{}
	req := &graphql.Request{Query: query, Variables: vars}
	resp := &graphql.Response{Data: _data}
	err = c.client.MakeRequest(ctx, req, resp)

	return _data, err
}

func (c *Client) CreateFolder(
	ctx context.Context,
	parentId string,
	folderName string,
	sharesLimit *int,
) (_data *CreateFolderResponse, err error) {
	// This is the GraphQL query as a string
	const query = `
	mutation createFolder($destination_id: String!, $name: String!, $shares_limit: Int = 1) {
	createFolder(destination_id: $destination_id, name: $name) {
		...Child
		parent {
		id
		name
		__typename
		}
		__typename
	}
	}

	fragment Child on Node {
	...BaseNode
	owner {
		id
		full_name
		email
		__typename
	}
	updated_at
	last_editor {
		id
		full_name
		email
		__typename
	}
	shares(limit: $shares_limit) {
		...Share
		__typename
	}
	__typename
	}

	fragment BaseNode on Node {
	id
	name
	type
	...Permissions
	... on File {
		size
		mime_type
		extension
		version
		__typename
	}
	flagged
	rootId
	__typename
	}

	fragment Permissions on Node {
	permissions {
		can_read
		can_write_file
		can_write_folder
		can_delete
		can_add_version
		can_read_link
		can_change_link
		can_share
		can_read_share
		can_change_share
		__typename
	}
	__typename
	}

	fragment Share on Share {
	permission
	share_target {
		... on User {
		email
		full_name
		id
		__typename
		}
		... on DistributionList {
		id
		name
		__typename
		}
		__typename
	}
	created_at
	__typename
	}
	`

	vars := map[string]interface{}{
		"destination_id": parentId,
		"name":           folderName,
	}
	if sharesLimit != nil {
		vars["shares_limit"] = *sharesLimit
	}

	_data = &CreateFolderResponse{}
	req := &graphql.Request{Query: query, Variables: vars}
	resp := &graphql.Response{Data: _data}
	err = c.client.MakeRequest(ctx, req, resp)

	return _data, err
}
