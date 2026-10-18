// Git integration: a background monitor keeps the focused pane's repository
// summary fresh for the status bar, and two commands show the full status and
// the uncommitted diff in the scrollable overlay. The monitor follows the same
// rule as the other background goroutines: it never touches the screen, it only
// updates state under a mutex and calls pane.Interrupt.
package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/atharva-0123-cs/tatami/agent"
	"github.com/atharva-0123-cs/tatami/git"
	"github.com/atharva-0123-cs/tatami/pane"
)

// gitProbeInterval is how often the focused pane's repository is re-read.
const gitProbeInterval = 3 * time.Second

// startGitMonitor launches the background repository poller.
func (a *App) startGitMonitor() {
	ctx, cancel := context.WithCancel(context.Background())
	a.gitCancel = cancel

	go func() {
		ticker := time.NewTicker(gitProbeInterval)
		defer ticker.Stop()
		last := ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cwd := a.focusedCwd()
				summary := ""
				if cwd != "" {
					if repo, err := git.Detect(cwd); err == nil {
						summary = repo.Summary()
					}
				}
				a.gitMu.Lock()
				a.gitRepoRoot = repoRoot(cwd)
				a.gitSummary = summary
				a.gitMu.Unlock()
				if summary != last {
					last = summary
					if a.events != nil {
						a.events.Publish(agent.Event{Kind: agent.EventGitStateChanged, Data: map[string]string{"summary": summary}})
					}
					pane.Interrupt()
				}
			}
		}
	}()
}

// statusExtra composes the status-bar's right-side subsystem indicators.
func (a *App) statusExtra() string {
	parts := make([]string, 0, 3)
	if g := a.graphStatusLabel(); g != "" {
		parts = append(parts, g)
	}
	if a.syncOn() {
		parts = append(parts, "sync")
	}
	return strings.Join(parts, " ")
}

// focusedCwd returns the working directory of the focused pane, or "".
func (a *App) focusedCwd() string {
	if p := a.wm.FocusedPane(); p != nil {
		return p.Cwd()
	}
	return ""
}

// repoRoot returns the repository root for cwd, or "".
func repoRoot(cwd string) string {
	if cwd == "" {
		return ""
	}
	if repo, err := git.Detect(cwd); err == nil {
		return repo.Root
	}
	return ""
}

// showGitStatus renders `git status` and `git diff --stat` in the overlay.
func (a *App) showGitStatus() {
	cwd := a.focusedCwd()
	repo, err := git.Detect(cwd)
	if err != nil {
		a.SetErrorMsg("Not a git repository")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), graphQueryTimeout)
	defer cancel()
	var b strings.Builder
	fmt.Fprintf(&b, "repository: %s\nbranch:     %s\ncommit:     %s\n",
		repo.Root, repo.Branch, repo.Head)
	if repo.Ahead > 0 || repo.Behind > 0 {
		fmt.Fprintf(&b, "ahead/behind: +%d / -%d\n", repo.Ahead, repo.Behind)
	}
	if repo.Worktree {
		b.WriteString("linked worktree: yes\n")
	}
	b.WriteString("\n")
	if status, err := repo.Status(ctx); err == nil {
		if strings.TrimSpace(status) == "" {
			b.WriteString("working tree clean\n")
		} else {
			b.WriteString(status)
		}
	}
	if stat, err := repo.DiffStat(ctx); err == nil && strings.TrimSpace(stat) != "" {
		b.WriteString("\n")
		b.WriteString(stat)
	}
	a.setOverlay("Git status", b.String())
}

// showGitDiff renders the uncommitted diff in the overlay.
func (a *App) showGitDiff() {
	cwd := a.focusedCwd()
	repo, err := git.Detect(cwd)
	if err != nil {
		a.SetErrorMsg("Not a git repository")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), graphQueryTimeout)
	defer cancel()
	diff, err := repo.DiffStat(ctx)
	if err != nil {
		a.SetErrorMsg(fmt.Sprintf("git diff failed: %v", err))
		return
	}
	if strings.TrimSpace(diff) == "" {
		diff = "(no uncommitted changes)"
	}
	a.setOverlay("Git diff", diff)
}
