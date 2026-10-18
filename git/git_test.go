package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDetect(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")

	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := Detect(sub)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if repo.Branch != "main" {
		t.Errorf("branch = %q, want main", repo.Branch)
	}
	if repo.Head == "" {
		t.Errorf("expected a head commit")
	}
	if repo.Dirty {
		t.Errorf("fresh repo should be clean")
	}

	// Dirty the tree.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err = Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !repo.Dirty {
		t.Errorf("expected dirty tree")
	}
	if repo.Summary() != "main*" {
		t.Errorf("Summary = %q, want main*", repo.Summary())
	}
}

func TestDetectNotARepo(t *testing.T) {
	if _, err := Detect(t.TempDir()); err != ErrNotARepo {
		t.Errorf("expected ErrNotARepo, got %v", err)
	}
}
