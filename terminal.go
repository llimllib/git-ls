package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"syscall"
	"unsafe"

	"github.com/mattn/go-runewidth"
)

var (
	BLUE      = "\x1b[34m"
	CYAN      = "\x1b[36m"
	GREEN     = "\x1b[32m"
	RED       = "\x1b[31m"
	RESET     = "\x1b[0m"
	YELLOW    = "\x1b[33m"
	STRIKEOUT = "\x1b[9m"
	NOCOLOR   = false
)

func init() {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		NOCOLOR = true
		BLUE = ""
		CYAN = ""
		GREEN = ""
		RED = ""
		RESET = ""
		YELLOW = ""
		STRIKEOUT = ""
	}
}

// validHashColors contains pre-computed 8-bit terminal color codes
// from the 6x6x6 RGB cube (16-231), excluding colors that are too dark,
// too light, or too gray.
var validHashColors = []int{
	18, 19, 20, 21, 24, 25, 26, 27, 28, 29,
	30, 31, 32, 33, 34, 35, 36, 37, 38, 39,
	40, 41, 42, 43, 44, 45, 46, 47, 48, 49,
	50, 51, 54, 55, 56, 57, 61, 62, 63, 64,
	67, 68, 69, 70, 71, 72, 73, 74, 75, 76,
	77, 78, 79, 80, 81, 82, 83, 84, 85, 86,
	87, 88, 89, 90, 91, 92, 93, 94, 97, 98,
	99, 100, 104, 105, 106, 107, 110, 111, 112, 113,
	114, 115, 116, 117, 118, 119, 120, 121, 122, 123,
	124, 125, 126, 127, 128, 129, 130, 131, 132, 133,
	134, 135, 136, 137, 140, 141, 142, 143, 147, 148,
	149, 150, 153, 154, 155, 156, 157, 158, 159, 160,
	161, 162, 163, 164, 165, 166, 167, 168, 169, 170,
	171, 172, 173, 174, 175, 176, 177, 178, 179, 180,
	183, 184, 185, 186, 190, 191, 192, 193, 196, 197,
	198, 199, 200, 201, 202, 203, 204, 205, 206, 207,
	208, 209, 210, 211, 212, 213, 214, 215, 216, 217,
	218, 219, 220, 221, 222, 223, 226, 227, 228, 229,
}

// hashToColor generates a color code for a commit hash.
// It uses 8-bit terminal colors (\e[38;5;<n>m) from the pre-computed validHashColors.
func hashToColor(hash string) string {
	if NOCOLOR {
		return ""
	}

	if hash == "" {
		return CYAN
	}

	// Calculate a numeric hash from the commit hash string
	var h uint32
	for i := 0; i < len(hash) && i < 8; i++ {
		h = h*31 + uint32(hash[i])
	}

	// Select a color from the pre-computed valid colors using the hash
	colorCode := validHashColors[h%uint32(len(validHashColors))]
	return fmt.Sprintf("\x1b[38;5;%dm", colorCode)
}

// link creates an OSC8 hyperlink escape sequence
func link(url string, name string) string {
	// hyperlink format: \e]8;;<url>\e\<link text>\e]8;;\e\
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, name)
}

// linkify converts issue references (#123) in a commit message to hyperlinks
func linkify(commitMsg string, github string, hash string) string {
	issueRe := regexp.MustCompile(`#(\d+)`)
	issueIx := issueRe.FindStringIndex(commitMsg)
	out := make([]string, 0, 16)
	for issueIx != nil {
		commitURL := fmt.Sprintf("%s/commit/%s", github, hash)
		out = append(out, link(commitURL, commitMsg[:issueIx[0]]))

		issueURL := fmt.Sprintf("%s/pull/%s", github, commitMsg[issueIx[0]+1:issueIx[1]])
		issueText := fmt.Sprintf("%s%s%s", BLUE, commitMsg[issueIx[0]:issueIx[1]], RESET)
		out = append(out, link(issueURL, issueText))

		commitMsg = commitMsg[issueIx[1]:]
		issueIx = issueRe.FindStringIndex(commitMsg)
	}
	out = append(out, link(fmt.Sprintf("%s/commit/%s", github, hash), commitMsg))

	return strings.Join(out, "")
}

// skipANSI checks if position i in string s is the start of an ANSI escape
// sequence. If so, it returns the number of bytes to skip (the length of the
// escape sequence). If not, it returns 0.
func skipANSI(s string, i int) int {
	if i >= len(s)-1 || s[i] != '\x1b' {
		return 0
	}

	// OSC sequence: \x1b]...\x1b\\
	if s[i+1] == ']' {
		end := strings.Index(s[i:], "\x1b\\")
		if end != -1 {
			return end + 2
		}
	}

	// CSI sequence: \x1b[...letter
	if s[i+1] == '[' {
		j := i + 2
		for j < len(s) {
			// @, A-Z, a-z terminate the escape
			if (s[j] >= 0x40 && s[j] <= 0x5a) || (s[j] >= 0x61 && s[j] <= 0x7a) {
				j++
				break
			}
			j++
		}
		return j - i
	}

	return 0
}

// stripANSI removes ANSI escape codes from a string
func stripANSI(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if n := skipANSI(s, i); n > 0 {
			i += n
			continue
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

// width returns the visible width of a string in a terminal, ignoring ANSI
// escape sequences and properly handling Unicode character widths.
func width(s string) int {
	return runewidth.StringWidth(stripANSI(s))
}

// truncateToWidth truncates a string to fit within the specified visual width,
// properly handling Unicode characters including diacritics and ANSI escape sequences.
// ANSI escape sequences (CSI and OSC) are passed through without counting toward width.
func truncateToWidth(s string, maxWidth int) string {
	if !strings.Contains(s, "\x1b") {
		// Fast path: no ANSI escapes, use runewidth directly
		return runewidth.Truncate(s, maxWidth, "")
	}

	// Slow path: ANSI-aware truncation
	var result strings.Builder
	visibleWidth := 0
	i := 0
	for i < len(s) && visibleWidth < maxWidth {
		if n := skipANSI(s, i); n > 0 {
			result.WriteString(s[i : i+n])
			i += n
			continue
		}

		// Regular character: decode rune and check width
		r, size := rune(s[i]), 1
		if s[i] >= 0x80 {
			// Multi-byte UTF-8
			for _, runeVal := range s[i:] {
				r = runeVal
				break
			}
			size = len(string(r))
		}

		rw := runewidth.RuneWidth(r)
		if visibleWidth+rw > maxWidth {
			break
		}
		result.WriteString(s[i : i+size])
		visibleWidth += rw
		i += size
	}

	// Drain any trailing ANSI escape sequences (e.g. RESET) that follow
	// the last visible character. These have zero visual width and must
	// be preserved to avoid color bleeding into subsequent columns.
	for i < len(s) {
		if n := skipANSI(s, i); n > 0 {
			result.WriteString(s[i : i+n])
			i += n
		} else {
			break
		}
	}

	return result.String()
}

type windowSize struct {
	rows uint16
	cols uint16
}

// terminalColumns returns the number of columns in the terminal
// from https://github.com/epam/hubctl/blob/6f86e6663/cmd/hub/lifecycle/terminal.go#L59
func terminalColumns(fd uintptr) int {
	var sz windowSize
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL,
		fd, uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&sz)))
	return int(sz.cols)
}
