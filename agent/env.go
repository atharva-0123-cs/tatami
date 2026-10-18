// Package agent implements tatami's built-in coding-agent harness: environment
// and project detection, a controlled command runner, file access under a
// policy, and a context protocol that packages terminal/pane/Git state for a
// model. Everything here is deterministic and testable; no model is required to
// exercise the harness.
package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"sort"

	"github.com/atharva-0123-cs/tatami/git"
)

// Environment is the machine-readable description of the host tatami runs in.
type Environment struct {
	OS              string            `json:"os"`
	Arch            string            `json:"arch"`
	Shell           string            `json:"shell"`
	Cwd             string            `json:"cwd"`
	Git             *GitInfo          `json:"git,omitempty"`
	Compilers       map[string]string `json:"compilers"`
	PackageManagers map[string]string `json:"package_managers"`
	BuildSystems    map[string]string `json:"build_systems"`
}

// GitInfo is the subset of repository state the harness reports.
type GitInfo struct {
	Root     string `json:"root"`
	Branch   string `json:"branch"`
	Head     string `json:"head"`
	Dirty    bool   `json:"dirty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Worktree bool   `json:"worktree"`
}

// toolGroups classifies the executables the harness probes for.
var toolGroups = struct {
	compilers       []string
	packageManagers []string
	buildSystems    []string
}{
	compilers:       []string{"go", "gcc", "g++", "clang", "clang++", "rustc", "python3", "python", "node", "deno", "bun", "java", "javac", "dotnet", "ruby", "php", "perl"},
	packageManagers: []string{"go", "npm", "yarn", "pnpm", "pip", "pip3", "poetry", "uv", "cargo", "gem", "composer", "mvn", "gradle", "apt", "brew"},
	buildSystems:    []string{"make", "cmake", "ninja", "bazel", "meson", "go", "cargo", "mvn", "gradle", "msbuild", "dotnet"},
}

// DetectEnvironment gathers OS/shell/cwd/Git and the available toolchains.
func DetectEnvironment(dir string) *Environment {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	env := &Environment{
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		Shell:           shellName(),
		Cwd:             dir,
		Compilers:       findTools(toolGroups.compilers),
		PackageManagers: findTools(toolGroups.packageManagers),
		BuildSystems:    findTools(toolGroups.buildSystems),
	}
	if repo, err := git.Detect(dir); err == nil {
		env.Git = &GitInfo{
			Root: repo.Root, Branch: repo.Branch, Head: repo.Head,
			Dirty: repo.Dirty, Ahead: repo.Ahead, Behind: repo.Behind, Worktree: repo.Worktree,
		}
	}
	return env
}

// shellName returns $SHELL's base name, falling back to a platform default.
func shellName() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return baseName(sh)
	}
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}

// baseName is a tiny path.Base that avoids importing path for one call.
func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

// findTools resolves a set of executables to their absolute paths, keeping only
// those present in $PATH. The result is non-nil and sorted for stable output.
func findTools(names []string) map[string]string {
	out := map[string]string{}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			out[n] = p
		}
	}
	return out
}

// JSON renders the environment as indented JSON, the harness's machine-readable
// form.
func (e *Environment) JSON() string {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

// ToolNames returns the sorted names of the tools in a category, useful for a
// human-readable summary.
func ToolNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
