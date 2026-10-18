package agent

import (
	"os"
	"path/filepath"
	"sort"
)

// ProjectType identifiers.
const (
	TypeGo      = "go"
	TypeC       = "c"
	TypeCPP     = "cpp"
	TypeRust    = "rust"
	TypePython  = "python"
	TypeNode    = "javascript"
	TypeJava    = "java"
	TypeUnknown = "unknown"
)

// Project is the harness's view of the repository/directory it works in.
type Project struct {
	Root        string   `json:"root"`
	Types       []string `json:"types"` // one or more ecosystem tags
	Metadata    Metadata `json:"metadata"`
	BuildConfig []string `json:"build_config"`
	TestConfig  []string `json:"test_config"`
}

// Metadata records the contextual files that guide an agent.
type Metadata struct {
	GitRepo  bool   `json:"git_repo"`
	Readme   string `json:"readme,omitempty"`
	AgentsMD string `json:"agents_md,omitempty"`
	ClaudeMD string `json:"claude_md,omitempty"`
	Cursor   bool   `json:"cursor"`
	GitHub   bool   `json:"github"`
}

// typeMarkers maps marker files to ecosystem tags.
var typeMarkers = []struct {
	file string
	typ  string
}{
	{"go.mod", TypeGo},
	{"Cargo.toml", TypeRust},
	{"pyproject.toml", TypePython},
	{"setup.py", TypePython},
	{"requirements.txt", TypePython},
	{"package.json", TypeNode},
	{"pom.xml", TypeJava},
	{"build.gradle", TypeJava},
	{"build.gradle.kts", TypeJava},
	{"CMakeLists.txt", TypeCPP},
	{"meson.build", TypeCPP},
	{"Makefile", TypeC},
}

// metadataFiles are the guidance/config files the harness reports.
var metadataFiles = struct {
	readmes []string
	agents  []string
	claude  []string
}{
	readmes: []string{"README.md", "README.rst", "README.txt", "README"},
	agents:  []string{"AGENTS.md"},
	claude:  []string{"CLAUDE.md"},
}

// buildConfigFiles are the files that describe how to build the project.
var buildConfigFiles = []string{
	"go.mod", "Cargo.toml", "pyproject.toml", "setup.py", "package.json",
	"pom.xml", "build.gradle", "build.gradle.kts", "CMakeLists.txt",
	"meson.build", "Makefile", "Dockerfile", "docker-compose.yml",
}

// testConfigFiles are the files/patterns that describe how to run tests.
var testConfigFiles = []string{
	"pytest.ini", "tox.ini", "jest.config.js", "jest.config.ts",
	"vitest.config.ts", "Makefile", "go.mod", "Cargo.toml",
}

// DetectProject inspects dir (and its parent, for Git) and returns the project
// description. It never fails: an unknown directory yields an "unknown" type.
func DetectProject(dir string) *Project {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	p := &Project{Root: dir}

	seen := map[string]bool{}
	for _, m := range typeMarkers {
		if fileExists(filepath.Join(dir, m.file)) && !seen[m.typ] {
			seen[m.typ] = true
			p.Types = append(p.Types, m.typ)
		}
	}
	if len(p.Types) == 0 {
		// Fall back to source-file extensions when no marker file exists.
		p.Types = detectByExtension(dir)
	}
	if len(p.Types) == 0 {
		p.Types = []string{TypeUnknown}
	}
	sort.Strings(p.Types)

	p.Metadata = detectMetadata(dir)
	for _, f := range buildConfigFiles {
		if fileExists(filepath.Join(dir, f)) {
			p.BuildConfig = append(p.BuildConfig, f)
		}
	}
	for _, f := range testConfigFiles {
		if fileExists(filepath.Join(dir, f)) {
			p.TestConfig = append(p.TestConfig, f)
		}
	}
	return p
}

// detectByExtension infers a type from the first recognized source file.
func detectByExtension(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	extToType := map[string]string{
		".go": TypeGo, ".c": TypeC, ".h": TypeC, ".cc": TypeCPP, ".cpp": TypeCPP,
		".hpp": TypeCPP, ".rs": TypeRust, ".py": TypePython, ".js": TypeNode,
		".ts": TypeNode, ".java": TypeJava,
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if t, ok := extToType[filepath.Ext(e.Name())]; ok {
			seen[t] = true
		}
	}
	var out []string
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// detectMetadata checks the contextual files.
func detectMetadata(dir string) Metadata {
	var m Metadata
	for _, d := range []string{dir, filepath.Dir(dir)} {
		if isDir(filepath.Join(d, ".git")) {
			m.GitRepo = true
			break
		}
	}
	for _, f := range metadataFiles.readmes {
		if fileExists(filepath.Join(dir, f)) {
			m.Readme = f
			break
		}
	}
	for _, f := range metadataFiles.agents {
		if fileExists(filepath.Join(dir, f)) {
			m.AgentsMD = f
		}
	}
	for _, f := range metadataFiles.claude {
		if fileExists(filepath.Join(dir, f)) {
			m.ClaudeMD = f
		}
	}
	m.Cursor = isDir(filepath.Join(dir, ".cursor"))
	m.GitHub = isDir(filepath.Join(dir, ".github"))
	return m
}

// fileExists reports whether path is an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// isDir reports whether path is an existing directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
