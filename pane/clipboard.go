package pane

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CopyToClipboard copies text to the system clipboard using the first
// available tool. It falls back to a temp file when no clipboard tool exists
// (common on bare TTYs / over SSH), so copied text is never silently lost.
func CopyToClipboard(text string) (string, error) {
	tools := [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"pbcopy"},
		{"termux-clipboard-set"},
	}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool[0]); err != nil {
			continue
		}
		cmd := exec.Command(tool[0], tool[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return tool[0], nil
		}
	}
	path := filepath.Join(os.TempDir(), "roge-copy.txt")
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		return "", fmt.Errorf("no clipboard tool found and temp-file fallback failed: %w", err)
	}
	return "", fmt.Errorf("no clipboard tool found; wrote %d bytes to %s", len(text), path)
}
