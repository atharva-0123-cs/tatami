package falkor

import "testing"

const sampleJS = `import React from 'react';
import { helper } from './helper';

const util = require('./util');

export class Button {
  render() {
    helper();
  }
}

export function mount() {
  new Button().render();
  util.run();
}

const format = (x) => util.trim(x);
`

func TestJSParser(t *testing.T) {
	g, err := (&JSParser{}).Parse("src/button.ts", []byte(sampleJS))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !hasNode(g, LabelFile, "language", "typescript") {
		t.Errorf("expected typescript language tag")
	}
	for _, imp := range []string{"react", "./helper", "./util"} {
		if !hasNode(g, LabelImport, "path", imp) {
			t.Errorf("missing import %q", imp)
		}
	}
	if !hasNode(g, LabelStruct, "name", "Button") {
		t.Errorf("missing class Button")
	}
	for _, fn := range []string{"render", "mount", "format"} {
		if !hasNode(g, LabelFunction, "name", fn) {
			t.Errorf("missing function %s", fn)
		}
	}
	if !hasNode(g, LabelFunction, "kind", "arrow") {
		t.Errorf("expected arrow function for format")
	}
}
