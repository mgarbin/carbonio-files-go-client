package graphql

// createFolder mutation response
type CreateFolderResponse struct {
	CreateFolder *Folder `json:"createFolder"`
}

type Folder struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Permissions Permissions `json:"permissions"`
	Flagged     bool        `json:"flagged"`
	RootID      string      `json:"rootId"`
	Owner       User        `json:"owner"`
	UpdatedAt   int64       `json:"updated_at"`
	LastEditor  *User       `json:"last_editor"` // nullable
	Shares      []Share     `json:"shares"`
	Parent      Parent      `json:"parent"`
	Typename    string      `json:"__typename"`
}

type Parent struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Typename string `json:"__typename"`
}
