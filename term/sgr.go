// SGR (Select Graphic Rendition) attribute handling for the emulator.
package term

// sgr applies Select Graphic Rendition attributes.
//
// Handles reset (0), bold (1), dim (2), italic (3), underline (4), reverse
// video (7), strikethrough (9), their negations (22/23/24/27/29), the 8 basic
// fg/bg colors (30-37/40-47), default fg/bg (39/49), bright colors
// (90-97/100-107), and the extended forms 38;5;N / 48;5;N (xterm palette) and
// 38;2;R;G;B / 48;2;R;G;B (truecolor). Unsupported attributes are skipped.
func (t *Terminal) sgr(nums []int) {
	if len(nums) == 0 {
		t.fg, t.bg = ColorDefault(), ColorDefault()
		t.bold, t.dim, t.italic, t.underline, t.rev, t.strikethrough = false, false, false, false, false, false
		return
	}
	for i := 0; i < len(nums); i++ {
		n := nums[i]
		if n < 0 {
			n = 0
		}
		switch {
		case n == 0:
			t.fg, t.bg = ColorDefault(), ColorDefault()
			t.bold, t.dim, t.italic, t.underline, t.rev, t.strikethrough = false, false, false, false, false, false
		case n == 1:
			t.bold = true
		case n == 2:
			t.dim = true
		case n == 3:
			t.italic = true
		case n == 4:
			t.underline = true
		case n == 7:
			t.rev = true
		case n == 9:
			t.strikethrough = true
		case n == 22:
			t.bold = false
			t.dim = false
		case n == 23:
			t.italic = false
		case n == 24:
			t.underline = false
		case n == 27:
			t.rev = false
		case n == 29:
			t.strikethrough = false
		case n >= 30 && n <= 37:
			t.fg = colorIndex(n - 30)
		case n == 39:
			t.fg = ColorDefault()
		case n >= 40 && n <= 47:
			t.bg = colorIndex(n - 40)
		case n == 49:
			t.bg = ColorDefault()
		case n >= 90 && n <= 97:
			t.fg = colorIndex(n - 90 + 8)
		case n >= 100 && n <= 107:
			t.bg = colorIndex(n - 100 + 8)
		case n == 38 || n == 48:
			isFg := n == 38
			if i+1 >= len(nums) {
				break
			}
			switch nums[i+1] {
			case 5:
				if i+2 < len(nums) && nums[i+2] >= 0 && nums[i+2] <= 255 {
					if isFg {
						t.fg = colorIndex(nums[i+2])
					} else {
						t.bg = colorIndex(nums[i+2])
					}
					i += 2
				} else {
					i += 2
				}
			case 2:
				if i+4 < len(nums) {
					if isFg {
						t.fg = colorRGB(nums[i+2], nums[i+3], nums[i+4])
					} else {
						t.bg = colorRGB(nums[i+2], nums[i+3], nums[i+4])
					}
				}
				i += 4
			default:
				i++
			}
		default:
			// 5/6 (blink), 8 (hidden), 53 (overline), ...: ignore.
		}
	}
}
