package services_test

import (
	"encoding/json"
	"testing"
)

// maxToolListBytes is the most the MCP tool list may weigh. Clients cache
// it, and once it grew past half a megabyte, agents were left with schemas
// weeks old even after reconnecting, most likely as their clients' refresh
// failed on its size, so it's kept well under that.
const maxToolListBytes = 250_000

// TestToolListSize checks that the MCP tool list stays small enough for
// clients to refresh: without output schemas, which GetHelp gives.
func TestToolListSize(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	data, err := json.Marshal(e.mcpTools)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > maxToolListBytes {
		t.Errorf("the tool list is %d bytes, want at most %d: trim descriptions, or exclude methods agents don't need", len(data), maxToolListBytes)
	}
	t.Logf("the tool list is %d bytes for %d tools", len(data), len(e.mcpTools))
}
