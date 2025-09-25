package graphql

type GetChildrenResponse struct {
	GetNode *Node `json:"getNode"`
}

type Node struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	Owner       *User        `json:"owner,omitempty"`
	Permissions *Permissions `json:"permissions,omitempty"`
	Flagged     *bool        `json:"flagged,omitempty"`
	RootID      *string      `json:"rootId,omitempty"`
	Children    *Children    `json:"children,omitempty"`
	UpdatedAt   *int64       `json:"updated_at,omitempty"`
	LastEditor  *User        `json:"last_editor,omitempty"`
	Shares      []*Share     `json:"shares,omitempty"`
	Size        *float64     `json:"size,omitempty"`
	MimeType    *string      `json:"mime_type,omitempty"`
	Extension   *string      `json:"extension,omitempty"`
	Version     *int         `json:"version,omitempty"`
	Typename    string       `json:"__typename"`
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
	CreatedAt   int64       `json:"created_at"`
	Typename    string      `json:"__typename"`
}

type ShareTarget struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Typename string `json:"__typename"`
}
