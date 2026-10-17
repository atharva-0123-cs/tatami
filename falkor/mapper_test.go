package falkor

import (
	"strings"
	"testing"

	"github.com/atharva-0123-cs/tatami/layout"
)

func TestLayoutToCypher(t *testing.T) {
	wm := layout.NewWindowManager()
	// Create an initial pane to have something to map
	err := wm.NewPane()
	if err != nil {
		t.Fatalf("Failed to create pane: %v", err)
	}
	
	stmts := LayoutToCypher(wm)
	
	found := false
	for _, s := range stmts {
		if strings.Contains(s, "CREATE (c:Pane") {
			found = true
			break
		}
	}
	
	if !found {
		t.Errorf("Cypher did not contain CREATE (c:Pane statement. Statements: %v", stmts)
	}
}
