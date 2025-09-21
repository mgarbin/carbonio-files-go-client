package graphql

type GetChildrenResponse struct {
	Data struct {
		GetNode *Node `json:"getNode"`
	} `json:"data"`
}

type Node struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	Owner       *User        `json:"owner,omitempty"`
	Permissions *Permissions `json:"permissions,omitempty"`
	Flagged     *bool        `json:"flagged,omitempty"`
	RootID      *string      `json:"rootId,omitempty"`
	Children    *Children    `json:"children,omitempty"` // For Folder nodes
	UpdatedAt   *string      `json:"updated_at,omitempty"`
	LastEditor  *User        `json:"last_editor,omitempty"`
	Shares      []*Share     `json:"shares,omitempty"`
	// File-specific
	Size      *int64  `json:"size,omitempty"`
	MimeType  *string `json:"mime_type,omitempty"`
	Extension *string `json:"extension,omitempty"`
	Version   *int    `json:"version,omitempty"`
	// Typename is always present for GraphQL
	Typename string `json:"__typename"`
}

type Children struct {
	Nodes     []*Node `json:"nodes"`
	PageToken *string `json:"page_token,omitempty"`
	Typename  string  `json:"__typename"`
}

type Permissions struct {
	CanRead        bool   `json:"can_read"`
	CanWriteFile   bool   `json:"can_write_file"`
	CanWriteFolder bool   `json:"can_write_folder"`
	CanDelete      bool   `json:"can_delete"`
	CanAddVersion  bool   `json:"can_add_version"`
	CanReadLink    bool   `json:"can_read_link"`
	CanChangeLink  bool   `json:"can_change_link"`
	CanShare       bool   `json:"can_share"`
	CanReadShare   bool   `json:"can_read_share"`
	CanChangeShare bool   `json:"can_change_share"`
	Typename       string `json:"__typename"`
}

type User struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Typename string `json:"__typename"`
}

type Share struct {
	Permission  string      `json:"permission"`
	ShareTarget ShareTarget `json:"share_target"`
	CreatedAt   string      `json:"created_at"`
	Typename    string      `json:"__typename"`
}

// ShareTarget can be User or DistributionList
type ShareTarget struct {
	User             *User             `json:"User,omitempty"`
	DistributionList *DistributionList `json:"DistributionList,omitempty"`
	Typename         string            `json:"__typename"`
}

type DistributionList struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Typename string `json:"__typename"`
}
