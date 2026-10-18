// Package git provides lightweight, read-only Git introspection for the
// status bar and the built-in agent harness. It shells out to the `git`
// binary, so it works with any git version and needs no cgo or library
// dependency; every call is bounded and degrades gracefully when git is
// missing or the directory is not a repository.
package git

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// gitTimeout bounds every git invocation so a hung git (e.g. waiting for
// credentials) can never freeze the UI or the agent.
const gitTimeout = 2 * time.Second

// ErrNotARepo is returned when dir is not inside a Git working tree.
var ErrNotARepo = errors.New("not a git repository")

// Repo describes the state of a Git working tree.
type Repo struct {
	Root     string // top level of the working tree
	Branch   string // current branch, or "HEAD" when detached
	Head     string // short commit hash
	Dirty    bool   // working tree has uncommitted changes
	Ahead    int    // commits ahead of upstream
	Behind   int    // commits behind upstream
	Worktree bool   // true when this is a linked worktree
}

// Detect finds the repository containing dir and reads its current state.
func Detect(dir string) (*Repo, error) {
	root, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNotARepo
	}
	r := &Repo{Root: strings.TrimSpace(root)}

	if branch, err := run(dir, "symbolic-ref", "--short", "HEAD"); err == nil {
		r.Branch = strings.TrimSpace(branch)
	} else {
		r.Branch = "HEAD"
	}
	if head, err := run(dir, "rev-parse", "--short", "HEAD"); err == nil {
		r.Head = strings.TrimSpace(head)
	}
	if wt, err := run(dir, "rev-parse", "--git-dir"); err == nil {
		if common, cerr := run(dir, "rev-parse", "--git-common-dir"); cerr == nil {
			r.Worktree = strings.TrimSpace(wt) != strings.TrimSpace(common)
		}
	}
	if out, err := run(dir, "status", "--porcelain"); err == nil {
		r.Dirty = strings.TrimSpace(out) != ""
	}
	if counts, err := run(dir, "rev-list", "--left-right", "--count", "@{upstream}...HEAD"); err == nil {
		fields := strings.Fields(strings.TrimSpace(counts))
		if len(fields) == 2 {
			r.Behind, _ = strconv.Atoi(fields[0])
			r.Ahead, _ = strconv.Atoi(fields[1])
		}
	}
	return r, nil
}

// Status returns the raw `git status --porcelain` output.
func (r *Repo) Status(ctx context.Context) (string, error) {
	if r == nil || r.Root == "" {
		return "", ErrNotARepo
	}
	return start(ctx, r.Root, "status", "--porcelain")
}

// DiffStat returns a short diffstat of uncommitted changes.
func (r *Repo) DiffStat(ctx context.Context) (string, error) {
	if r == nil || r.Root == "" {
		return "", ErrNotARepo
	}
	return start(ctx, r.Root, "diff", "--stat")
}

// Summary renders a one-line description suitable for a status bar, for
// example "main* +1 -2". A trailing "*" marks a dirty tree.
func (r *Repo) Summary() string {
	if r == nil {
		return ""
	}
	s := r.Branch
	if r.Dirty {
		s += "*"
	}
	if r.Ahead > 0 {
		s += " +" + strconv.Itoa(r.Ahead)
	}
	if r.Behind > 0 {
		s += " -" + strconv.Itoa(r.Behind)
	}
	return s
}

// run executes git in dir with a short timeout and returns stdout.
func run(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	return start(ctx, dir, args...)
}

// start executes git under the caller's context.
func start(ctx context.Context, dir string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, "git", args...)
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
