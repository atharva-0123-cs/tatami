package falkor

import "testing"

const samplePython = `import os
from typing import List

class Widget:
    def render(self):
        print("render")
        self.draw()

    def draw(self):
        helper()

def helper():
    os.getcwd()

def top():
    render_helper()
`

func TestPythonParser(t *testing.T) {
	g, err := (&PythonParser{}).Parse("pkg/widget.py", []byte(samplePython))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !hasNode(g, LabelFile, "path", "pkg/widget.py") {
		t.Errorf("missing File node")
	}
	if !hasNode(g, LabelImport, "path", "os") {
		t.Errorf("missing import os")
	}
	if !hasNode(g, LabelImport, "path", "typing") {
		t.Errorf("missing import typing")
	}
	if !hasNode(g, LabelStruct, "name", "Widget") {
		t.Errorf("missing class Widget")
	}
	for _, fn := range []string{"render", "draw", "helper", "top"} {
		if !hasNode(g, LabelFunction, "name", fn) {
			t.Errorf("missing function %s", fn)
		}
	}

	// draw() is called from render(), and both are declared in this file.
	g.Resolve()
	renderID := symbolNodeID("pkg/widget.py", "func", "render")
	drawID := symbolNodeID("pkg/widget.py", "func", "draw")
	if !hasEdge(g, renderID, drawID, RelCalls) {
		t.Errorf("expected render -CALLS-> draw")
	}
}
