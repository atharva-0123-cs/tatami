package agent

import "testing"

func TestRegistryLifecycle(t *testing.T) {
	r := NewRegistry()
	a, err := r.Start("alpha", t.TempDir(), Policy{}, "pane-1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if a.SessionID == "" || r.ActiveName() != "alpha" {
		t.Fatalf("bad initial active/session: %q %q", a.SessionID, r.ActiveName())
	}
	if _, err := r.Start("alpha", "", Policy{}, ""); err == nil {
		t.Fatal("expected duplicate-name error")
	}
	b, err := r.Start("beta", t.TempDir(), Policy{}, "pane-2")
	if err != nil {
		t.Fatalf("start beta: %v", err)
	}
	if err := r.Switch("beta"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if r.Active().Name != "beta" {
		t.Fatalf("active = %q, want beta", r.ActiveName())
	}
	r.Touch("beta")
	list := r.List()
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	var beta Status
	for _, s := range list {
		if s.Name == "beta" {
			beta = s
		}
	}
	if beta.Commands != 1 || !beta.Active {
		t.Fatalf("beta status = %+v", beta)
	}
	if b.SessionID == a.SessionID {
		t.Fatal("session ids must be unique")
	}
	if err := r.Stop("beta"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if r.Get("beta") != nil {
		t.Fatal("beta should be gone")
	}
	if r.ActiveName() != "alpha" {
		t.Fatalf("active after stop = %q, want alpha", r.ActiveName())
	}
}

func TestResultIDsAndTiming(t *testing.T) {
	r := NewRunner(t.TempDir(), Policy{})
	res1 := r.RunShell(t.Context(), "true")
	res2 := r.RunShell(t.Context(), "true")
	if res1.ID == "" || res1.ID == res2.ID {
		t.Fatalf("command ids not unique: %q %q", res1.ID, res2.ID)
	}
	if res1.Finished.Before(res1.Started) {
		t.Fatal("finished before started")
	}
	if !res1.Success() {
		t.Fatalf("true should succeed: %+v", res1)
	}
}
