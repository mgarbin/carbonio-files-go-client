package graphql

// moveNodes mutation response
type MoveNodesResponse struct {
	MoveNodes []struct {
		ID     string `json:"id"`
		Parent struct {
			ID string `json:"id"`
		} `json:"parent"`
	} `json:"moveNodes"`
}
