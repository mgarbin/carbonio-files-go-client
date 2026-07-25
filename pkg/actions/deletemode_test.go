package actions

import "testing"

// TestResolveDeleteMode covers LiveCacheSync's deleteRemoteNode normalization:
// only DeleteModeDelete opts into permanent deletion, every other input
// (empty, garbage, DeleteModeTrash itself) must fall back to
// DeleteModeTrash so a missing/legacy preference never turns into a
// permanent deletion.
func TestResolveDeleteMode(t *testing.T) {
	cases := map[string]string{
		"":               DeleteModeTrash,
		DeleteModeTrash:  DeleteModeTrash,
		DeleteModeDelete: DeleteModeDelete,
		"garbage":        DeleteModeTrash,
		"Delete":         DeleteModeTrash, // case-sensitive: only the exact constant opts in
	}
	for input, want := range cases {
		if got := resolveDeleteMode(input); got != want {
			t.Fatalf("resolveDeleteMode(%q) = %q, want %q", input, got, want)
		}
	}
}
