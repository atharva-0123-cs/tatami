// Scrollback, copy-mode view, and text-extraction helpers for the emulator.
package term

import "strings"

// scrollUp moves region lines up; the top line enters scrollback when the
// region is the whole main screen.
//
// scrollback capture only happens for the full main screen (no alt buffer, no
// partial region) so that scrolling in vim or less does not pollute yank
// history, while plain shell output is fully recoverable.
func (t *Terminal) scrollUp(n int) {
	if n < 1 {
		return
	}
	if n > t.bot-t.top+1 {
		n = t.bot - t.top + 1
	}
	fullMain := !t.altOn && t.top == 0 && t.bot == t.rows-1
	for k := 0; k < n; k++ {
		if fullMain {
			t.pushScrollback(t.gridRow(0))
		}
		copy(t.grid[t.top*t.cols:(t.bot+1)*t.cols-t.cols], t.grid[(t.top+1)*t.cols:(t.bot+1)*t.cols])
		t.clearRow(t.bot)
	}
}

// scrollDown moves region lines down (reverse scroll).
func (t *Terminal) scrollDown(n int) {
	if n < 1 {
		return
	}
	if n > t.bot-t.top+1 {
		n = t.bot - t.top + 1
	}
	for k := 0; k < n; k++ {
		copy(t.grid[(t.top+1)*t.cols:(t.bot+1)*t.cols], t.grid[t.top*t.cols:(t.bot+1)*t.cols-t.cols])
		t.clearRow(t.top)
	}
}

// pushScrollback copies a line into scrollback, trimming the buffer to the cap.
func (t *Terminal) pushScrollback(row []Cell) {
	cp := make([]Cell, len(row))
	copy(cp, row)
	t.scrollback = append(t.scrollback, cp)
	if len(t.scrollback) > t.scrollCap {
		t.scrollback = t.scrollback[len(t.scrollback)-t.scrollCap:]
	}
	// Keep the copy-mode view offset valid as lines scroll away.
	if t.scrollOff > len(t.scrollback) {
		t.scrollOff = len(t.scrollback)
	}
}

// eraseDisplay implements ED (CSI J): 0 = cursor to end, 1 = start to cursor,
// 2 = whole screen, 3 = whole screen plus scrollback.
//
// Mode 3 also clears the scrollback buffer so the shell can wipe yank history.
func (t *Terminal) eraseDisplay(mode int) {
	switch mode {
	case 1: // cursor to start.
		for y := 0; y < t.y; y++ {
			t.clearRow(y)
		}
		for x := 0; x <= t.x && x < t.cols; x++ {
			*t.at(x, t.y) = blankCell()
		}
	case 2, 3: // whole screen (+scrollback for 3J).
		for y := 0; y < t.rows; y++ {
			t.clearRow(y)
		}
		if mode == 3 && !t.altOn {
			t.scrollback = nil
		}
	default: // 0: cursor to end.
		for x := t.x; x < t.cols; x++ {
			*t.at(x, t.y) = blankCell()
		}
		for y := t.y + 1; y < t.rows; y++ {
			t.clearRow(y)
		}
	}
}

// eraseLine clears the current line: 0 = cursor to end, 1 = start to cursor, 2 = whole line.
func (t *Terminal) eraseLine(mode int) {
	switch mode {
	case 1:
		for x := 0; x <= t.x && x < t.cols; x++ {
			*t.at(x, t.y) = blankCell()
		}
	case 2:
		t.clearRow(t.y)
	default:
		for x := t.x; x < t.cols; x++ {
			*t.at(x, t.y) = blankCell()
		}
	}
}

// insertLines opens n blank lines at the cursor row within the scroll region.
//
// Lines at and below the cursor shift down; any that fall past the region's
// bottom are clipped. Used by apps for scrolling panes inside a region.
func (t *Terminal) insertLines(n int) {
	if t.y < t.top || t.y > t.bot || n < 1 {
		return
	}
	if n > t.bot-t.y+1 {
		n = t.bot - t.y + 1
	}
	for k := 0; k < n; k++ {
		copy(t.grid[(t.y+1)*t.cols:(t.bot+1)*t.cols], t.grid[t.y*t.cols:(t.bot+1)*t.cols-t.cols])
		for x := 0; x < t.cols; x++ {
			*t.at(x, t.y) = blankCell()
		}
	}
}

// deleteLines removes n lines at the cursor row, pulling lines below into the region.
func (t *Terminal) deleteLines(n int) {
	if t.y < t.top || t.y > t.bot || n < 1 {
		return
	}
	if n > t.bot-t.y+1 {
		n = t.bot - t.y + 1
	}
	for k := 0; k < n; k++ {
		copy(t.grid[t.y*t.cols:(t.bot+1)*t.cols-t.cols], t.grid[(t.y+1)*t.cols:(t.bot+1)*t.cols])
		t.clearRow(t.bot)
	}
}

// insertChars shifts the cursor row right, blanking the cells vacated at the left.
func (t *Terminal) insertChars(n int) {
	if n < 1 || t.x >= t.cols {
		return
	}
	if n > t.cols-t.x {
		n = t.cols - t.x
	}
	row := t.gridRow(t.y)
	copy(row[t.x+n:], row[t.x:t.cols-n])
	for x := t.x; x < t.x+n; x++ {
		row[x] = blankCell()
	}
}

// deleteChars shifts the cursor row left, blanking the cells vacated at the right.
func (t *Terminal) deleteChars(n int) {
	if n < 1 || t.x >= t.cols {
		return
	}
	if n > t.cols-t.x {
		n = t.cols - t.x
	}
	row := t.gridRow(t.y)
	copy(row[t.x:], row[t.x+n:])
	for x := t.cols - n; x < t.cols; x++ {
		row[x] = blankCell()
	}
}

// eraseChars blanks n cells at and after the cursor without shifting the row.
func (t *Terminal) eraseChars(n int) {
	if n < 1 {
		return
	}
	for k := 0; k < n && t.x+k < t.cols; k++ {
		*t.at(t.x+k, t.y) = blankCell()
	}
}

// ------------------------------------------------------------- reading -----

// SetScrollOffset sets the copy-mode view offset: how many scrollback lines
// are shown above the screen. Clamped to [0, len(scrollback)]. A zero offset
// shows the live screen.
func (t *Terminal) SetScrollOffset(n int) {
	if n < 0 {
		n = 0
	}
	if n > len(t.scrollback) {
		n = len(t.scrollback)
	}
	t.scrollOff = n
}

// ScrollOffset returns the current copy-mode view offset.
func (t *Terminal) ScrollOffset() int { return t.scrollOff }

// viewRowCount returns how many rows are visible in the current view
// (scrollback lines scrolled in plus the live grid rows).
func (t *Terminal) viewRowCount() int {
	n := t.scrollOff + t.rows
	if n > len(t.scrollback)+t.rows {
		n = len(t.scrollback) + t.rows
	}
	return n
}

// viewRow returns the cell row visible at view row yy (0 = first visible).
//
// Rows from the scrollback fill the top of the view (newest scrollback line
// sits just above the live grid); once the scrollback window is exhausted the
// live grid rows follow.
func (t *Terminal) viewRow(yy int) []Cell {
	if yy < t.scrollOff {
		return t.scrollback[len(t.scrollback)-t.scrollOff+yy]
	}
	return t.gridRow(yy - t.scrollOff)
}

// atView returns the cell at view coordinates (xx, yy), where yy counts from
// the top of the scrolled view.
func (t *Terminal) atView(xx, yy int) *Cell {
	if yy < t.scrollOff {
		return &t.scrollback[len(t.scrollback)-t.scrollOff+yy][xx]
	}
	return t.at(xx, yy-t.scrollOff)
}

// lineText renders one stored line as plain text without trailing blanks.
//
// NUL cells render as spaces; trailing spaces are trimmed so that joined lines
// do not accumulate padding. Used by dump/allText for tests and copy.
func lineText(row []Cell) string {
	end := len(row)
	for end > 0 && row[end-1].Ch == ' ' {
		end--
	}
	var sb strings.Builder
	for _, c := range row[:end] {
		ch := c.Ch
		if ch == 0 {
			ch = ' '
		}
		sb.WriteRune(ch)
	}
	return sb.String()
}

// allText returns scrollback + screen as plain text (for yank/copy).
//
// Scrollback lines come first (each followed by a newline), then the visible
// grid rows. Wide characters appear as their lead rune; a trailing newline is
// trimmed so the result is line-oriented.
func (t *Terminal) allText() string {
	var sb strings.Builder
	for _, row := range t.scrollback {
		sb.WriteString(lineText(row))
		sb.WriteByte('\n')
	}
	grid := t.grid
	if t.altOn {
		grid = t.grid // alt has no scrollback; copy the visible screen.
	}
	for y := 0; y < t.rows; y++ {
		sb.WriteString(lineText(grid[y*t.cols : (y+1)*t.cols]))
		if y < t.rows-1 {
			sb.WriteByte('\n')
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// dump returns the visible grid as plain text (used by tests).
//
// Lines are rendered like a real terminal's scroll buffer: trailing blanks are
// trimmed and the final newline is dropped.
func (t *Terminal) dump() string {
	var sb strings.Builder
	for y := 0; y < t.rows; y++ {
		sb.WriteString(lineText(t.gridRow(y)))
		if y < t.rows-1 {
			sb.WriteByte('\n')
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
