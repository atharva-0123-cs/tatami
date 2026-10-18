package agent

import (
	"fmt"
	"strings"
)

// Policy is the permission/sandbox model enforced by the Runner and the file
// tools. Its zero value allows every command that is not obviously destructive;
// set fields to tighten it. Trusted commands run without confirmation; denied
// commands never run; destructive commands additionally require confirmation
// when RequireConfirmation is set.
type Policy struct {
	// Allow, when non-empty, is an allowlist of command names (argv[0]); any
	// command not listed is refused.
	Allow []string
	// Deny is a denylist of command names that are always refused.
	Deny []string
	// DenyPatterns refuse any command whose full text contains one of these
	// substrings (e.g. "rm -rf /").
	DenyPatterns []string
	// RequireConfirmation gates destructive commands behind Confirm.
	RequireConfirmation bool
	// Confirm approves a destructive command; nil means deny when confirmation
	// is required.
	Confirm func(command string) bool
	// ReadOnly forbids commands that typically mutate the filesystem or
	// repository.
	ReadOnly bool
	// Network forbids commands that make network requests.
	Network bool
	// EnvAllow, when non-empty, limits which inherited environment variables are
	// passed to child processes.
	EnvAllow []string
}

// destructiveCommands are recognized as mutating or dangerous.
var destructiveCommands = map[string]bool{
	"rm": true, "rmdir": true, "dd": true, "mkfs": true, "shred": true,
	"mv": true, "chmod": true, "chown": true, "chgrp": true, "truncate": true,
	"kill": true, "pkill": true, "killall": true, "shutdown": true, "reboot": true,
	"sudo": true, "su": true, "tee": true, "install": true, "ln": true,
}

// networkCommands require network access.
var networkCommands = map[string]bool{
	"curl": true, "wget": true, "ssh": true, "scp": true, "rsync": true,
	"nc": true, "telnet": true, "ping": true, "git": true,
}

// Check validates a command against the policy, returning a descriptive error
// when it is refused.
func (p *Policy) Check(name string, args []string) error {
	full := strings.TrimSpace(name + " " + strings.Join(args, " "))
	for _, d := range p.Deny {
		if d == name {
			return fmt.Errorf("command %q is denied by policy", name)
		}
	}
	if len(p.Allow) > 0 && !contains(p.Allow, name) {
		return fmt.Errorf("command %q is not in the allowlist", name)
	}
	for _, pat := range p.DenyPatterns {
		if pat != "" && strings.Contains(full, pat) {
			return fmt.Errorf("command matches denied pattern %q", pat)
		}
	}
	if p.Network && networkCommands[name] {
		return fmt.Errorf("command %q is blocked by the no-network policy", name)
	}
	if p.ReadOnly && (destructiveCommands[name] || isWriteCommand(name, args)) {
		return fmt.Errorf("command %q is blocked by the read-only policy", name)
	}
	if p.RequireConfirmation && LooksDestructive(name, args) {
		if p.Confirm == nil || !p.Confirm(full) {
			return fmt.Errorf("destructive command %q was not confirmed", full)
		}
	}
	return nil
}

// LooksDestructive reports whether a command mutates state or is dangerous.
func LooksDestructive(name string, args []string) bool {
	if destructiveCommands[name] {
		return true
	}
	if name == "git" {
		for _, a := range args {
			switch {
			case a == "reset" || a == "clean" || a == "checkout" || a == "revert":
				return true
			case strings.HasPrefix(a, "--force") || strings.HasPrefix(a, "-f"):
				return true
			}
		}
	}
	return isWriteCommand(name, args)
}

// isWriteCommand detects commands/arguments that write to the filesystem.
func isWriteCommand(name string, args []string) bool {
	switch name {
	case "install", "mkfifo":
		return true
	}
	for _, a := range args {
		if a == ">" || strings.HasPrefix(a, ">>") {
			return true
		}
	}
	return false
}

// contains reports whether s is in list.
func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
