package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxReadBytes caps a single file read so the agent can never pull an enormous
// file into its context.
const MaxReadBytes = 256 * 1024

// FileTools gives an agent policy-checked read/inspect/write access to files
// under a root directory. Reads and writes are confined to the root, and writes
// are refused entirely under a read-only policy.
type FileTools struct {
	Root   string
	Policy Policy
}

// NewFileTools creates file tools rooted at dir.
func NewFileTools(dir string, p Policy) *FileTools {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	return &FileTools{Root: dir, Policy: p}
}

// FileInfo is the inspection result for a path.
type FileInfo struct {
	Path    string
	Size    int64
	IsDir   bool
	Mode    string
	Preview string // first few lines for text files
}

// resolve turns a possibly relative path into an absolute one and refuses any
// path that escapes the root.
func (f *FileTools) resolve(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(f.Root, abs)
	}
	abs = filepath.Clean(abs)
	root := filepath.Clean(f.Root)
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace root", path)
	}
	return abs, nil
}

// List returns the entries of a directory (names only, sorted).
func (f *FileTools) List(dir string) ([]string, error) {
	abs, err := f.resolve(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
	}
	return out, nil
}

// Inspect returns metadata plus a short text preview.
func (f *FileTools) Inspect(path string) (FileInfo, error) {
	abs, err := f.resolve(path)
	if err != nil {
		return FileInfo{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return FileInfo{}, err
	}
	fi := FileInfo{Path: abs, Size: info.Size(), IsDir: info.IsDir(), Mode: info.Mode().String()}
	if !info.IsDir() && info.Size() <= MaxReadBytes && looksTextual(abs) {
		data, err := os.ReadFile(abs)
		if err == nil {
			lines := strings.Split(string(data), "\n")
			if len(lines) > 20 {
				lines = lines[:20]
			}
			fi.Preview = strings.Join(lines, "\n")
		}
	}
	return fi, nil
}

// Read returns a file's contents, capped at MaxReadBytes.
func (f *FileTools) Read(path string) (string, error) {
	abs, err := f.resolve(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	if len(data) > MaxReadBytes {
		data = data[:MaxReadBytes]
	}
	return string(data), nil
}

// Write creates or replaces a file. It is refused under a read-only policy or
// when the policy denies file writes.
func (f *FileTools) Write(path, content string) error {
	if f.Policy.ReadOnly {
		return fmt.Errorf("write to %q refused: read-only policy", path)
	}
	abs, err := f.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0o644)
}

// looksTextual does a cheap binary check so previews never dump control bytes.
func looksTextual(path string) bool {
	fd, err := os.Open(path)
	if err != nil {
		return false
	}
	defer fd.Close()
	buf := make([]byte, 512)
	n, _ := fd.Read(buf)
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return false
		}
	}
	return true
}
