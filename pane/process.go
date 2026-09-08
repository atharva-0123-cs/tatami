// Shell-process spawning helpers used by NewPane (pane.go): candidate shell
// selection, environment plumbing, and the actual PTY start.
package pane

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/creack/pty"
)

// shellCandidates returns the shells to try, preferring $SHELL, deduped and
// verified to exist in $PATH.
//
// The order is: $SHELL if set, then bash, zsh, sh. Candidates that cannot be
// resolved are logged and skipped so NewPane can quietly fall through to a
// shell that does exist.
func shellCandidates() []string {
	var candidates []string
	seen := map[string]bool{}
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		if _, err := exec.LookPath(s); err != nil {
			log.Printf("Shell candidate %q not found, skipping: %v", s, err)
			return
		}
		seen[s] = true
		candidates = append(candidates, s)
	}
	add(os.Getenv("SHELL"))
	add("bash")
	add("zsh")
	add("sh")
	return candidates
}

// setEnv returns env with key=val set, replacing any existing entry instead of
// appending a duplicate.
//
// os.Environ() may already contain "TERM=..."; replacing in place (rather than
// appending a second entry) keeps the variable list small and unambiguous.
func setEnv(env []string, key, val string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + val
			return env
		}
	}
	return append(env, prefix+val)
}

// spawnShell starts the first working candidate shell in a fresh PTY and
// returns the PTY master plus the started command.
//
// It iterates shellCandidates in order, setting the working directory and
// forcing TERM=xterm-256color for full color support. It fails only if no
// candidate shell could be started (the last error is wrapped for diagnosis).
func spawnShell(cwd string) (*os.File, *exec.Cmd, error) {
	shells := shellCandidates()

	var lastErr error
	for _, sh := range shells {
		cmd := exec.Command(sh)
		cmd.Dir = cwd
		cmd.Env = setEnv(os.Environ(), "TERM", "xterm-256color")
		m, serr := pty.Start(cmd)
		if serr == nil {
			return m, cmd, nil
		}
		lastErr = serr
		log.Printf("Warning: failed to start shell %q: %v", sh, serr)
	}

	return nil, nil, fmt.Errorf(
		"failed to start any shell (tried %s): %w. Ensure a login shell is available and that PTY creation is permitted in this environment",
		strings.Join(shells, ", "), lastErr,
	)
}
