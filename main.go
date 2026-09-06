package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func main() {
	app := tview.NewApplication()

	// Customize borders to have rounded (circular) corners
	tview.Borders.TopLeft = tview.BoxDrawingsLightArcDownAndRight
	tview.Borders.TopRight = tview.BoxDrawingsLightArcDownAndLeft
	tview.Borders.BottomLeft = tview.BoxDrawingsLightArcUpAndRight
	tview.Borders.BottomRight = tview.BoxDrawingsLightArcUpAndLeft

	// Right panel: Output / Diff / Logs view
	outputView := tview.NewTextView().
		SetDynamicColors(true)
	outputView.SetText("[green]Welcome to Mini-Lazygit (tview edition)![white]\n\n" +
		"Shortcuts:\n" +
		" • [yellow]r[white] - Refresh git status\n" +
		" • [yellow]s[white] - Show git status\n" +
		" • [yellow]l[white] - Show git log\n" +
		" • [yellow]Enter[white] - Stage file\n" +
		" • [yellow]Tab[white] - Switch focus between panels\n" +
		" • [yellow]q[white] - Quit application")
	outputView.SetBorder(true).
		SetTitle(" [yellow]Output / Diff[white] ").
		SetTitleAlign(tview.AlignLeft)

	// Left panel: Git Status (Files list)
	fileList := tview.NewList().
		ShowSecondaryText(true)

	// Function to refresh the file list from `git status`
	var refreshStatus func()
	refreshStatus = func() {
		fileList.Clear()
		cmd := exec.Command("git", "status", "--porcelain")
		out, err := cmd.Output()
		if err != nil {
			fileList.AddItem("Not a git repository", "Run inside a git repo", 0, nil)
			return
		}

		lines := strings.Split(string(out), "\n")
		count := 0
		for _, line := range lines {
			if len(line) > 3 {
				status := line[:2]
				filename := strings.TrimSpace(line[3:])

				statusDesc := "Modified"
				if strings.Contains(status, "A") {
					statusDesc = "Added"
				} else if strings.Contains(status, "?") {
					statusDesc = "Untracked"
				}

				fn := filename
				fileList.AddItem(filename, statusDesc, 0, func() {
					// Pressing Enter on a file stages it
					stageCmd := exec.Command("git", "add", fn)
					if err := stageCmd.Run(); err != nil {
						outputView.SetText(fmt.Sprintf("[red]Error staging %s: %v[white]", fn, err))
						return
					}
					refreshStatus()
					outputView.SetText(fmt.Sprintf("[green]Staged file:[white] %s", fn))
				})
				count++
			}
		}

		if count == 0 {
			fileList.AddItem("(Working tree clean)", "No changes detected", 0, nil)
		}
	}

	refreshStatus()

	fileList.SetBorder(true).
		SetTitle(" [yellow]Git Status (Files)[white] ").
		SetTitleAlign(tview.AlignLeft)

	// Layout: Flex container splitting screen horizontally
	flex := tview.NewFlex().
		AddItem(fileList, 0, 1, true).
		AddItem(outputView, 0, 2, false)

	// Focus management for Tab switching
	currentFocus := 0
	primitives := []tview.Primitive{fileList, outputView}

	// Global keybindings
	flex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			currentFocus = (currentFocus + 1) % len(primitives)
			app.SetFocus(primitives[currentFocus])
			return nil
		}

		switch event.Rune() {
		case 'q':
			app.Stop()
		case 'r':
			refreshStatus()
			outputView.SetText("[green]Git status refreshed.[white]")
		case 's':
			cmd := exec.Command("git", "status")
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Run()
			outputView.SetText(out.String())
		case 'l':
			cmd := exec.Command("git", "log", "-n", "30", "--oneline")
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Run()
			outputView.SetText(out.String())
		}

		return event
	})

	if err := app.SetRoot(flex, true).SetFocus(fileList).Run(); err != nil {
		panic(err)
	}
}
