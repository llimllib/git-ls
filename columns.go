package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-runewidth"
)

// truncateToWidth truncates a string to fit within the specified visual width,
// properly handling Unicode characters including diacritics
func truncateToWidth(s string, maxWidth int) string {
	return runewidth.Truncate(s, maxWidth, "")
}

// Nerd Font icons for individual git status characters.
// See https://www.nerdfonts.com/cheat-sheet for the full icon list.
//
// Git's porcelain status is two characters: [index][worktree].
// The index (staged) character is colored green, worktree (unstaged) is red,
// matching git's own color convention.
var nerdFontCharMap = map[byte]string{
	'M': "\uf040", // nf-fa-pencil (modified)
	'A': "\uf471", // nf-oct-diff_added (added)
	'D': "\uf014", // nf-fa-trash_o (deleted)
	'R': "\uf064", // nf-fa-share (renamed/moved)
	'C': "\uf0c5", // nf-fa-copy (copied)
	'U': "\uf0e7", // nf-fa-bolt (unmerged)
}

// nerdFontSpecialMap handles statuses that don't follow the 2-char
// [index][worktree] pattern, or where the two characters don't represent
// separate staged/unstaged states.
var nerdFontSpecialMap = map[string]string{
	"I":  "\uf070", // nf-fa-eye_slash (ignored)
	"*":  "\ue5fb", // nf-custom-folder_git (.git directory)
	"??": "\uf128", // nf-fa-question (untracked — not a staged/unstaged split)
	"UU": "\uf0e7", // nf-fa-bolt (unmerged, both modified)
	"AA": "\uf0e7", // nf-fa-bolt (unmerged, both added)
	"DD": "\uf0e7", // nf-fa-bolt (unmerged, both deleted)
	"AU": "\uf0e7", // nf-fa-bolt (unmerged, added by us)
	"UA": "\uf0e7", // nf-fa-bolt (unmerged, added by them)
	"DU": "\uf0e7", // nf-fa-bolt (unmerged, deleted by us)
	"UD": "\uf0e7", // nf-fa-bolt (unmerged, deleted by them)
}

// statusToNerdFont converts a git status string to a colored Nerd Font icon
// string. For 2-character statuses, the index (first) character is shown in
// green and the worktree (second) character in red, preserving the
// staged-vs-unstaged distinction. For compound statuses (comma-separated),
// each part is converted independently.
func statusToNerdFont(status string) string {
	if status == "" {
		return ""
	}

	// Handle compound statuses like "M , D"
	parts := strings.Split(status, ",")
	var result strings.Builder
	for _, part := range parts {
		result.WriteString(statusPartToNerdFont(part))
	}
	return result.String()
}

// statusPartToNerdFont converts a single status code to nerdfont icons.
// All returned strings include a trailing space to visually separate the
// status icons from the adjacent diff column.
func statusPartToNerdFont(status string) string {
	// Check special statuses first
	if icon, ok := nerdFontSpecialMap[status]; ok {
		return icon + " "
	}

	// Standard 2-char status: [index][worktree]
	if len(status) == 2 {
		idx, wt := status[0], status[1]
		idxIcon, hasIdx := nerdFontCharMap[idx]
		wtIcon, hasWt := nerdFontCharMap[wt]

		if hasIdx || hasWt {
			var result strings.Builder

			// Index (staged) character — green
			if hasIdx {
				result.WriteString(GREEN)
				result.WriteString(idxIcon)
				result.WriteString(RESET)
			}

			// Separate icons with a space to prevent terminal
			// rendering glitches with adjacent PUA glyphs
			if hasIdx && hasWt {
				result.WriteByte(' ')
			}

			// Worktree (unstaged) character — red
			if hasWt {
				result.WriteString(RED)
				result.WriteString(wtIcon)
				result.WriteString(RESET)
			}

			// Trailing space to visually separate from the next column
			result.WriteByte(' ')

			return result.String()
		}
	}

	// Fallback: return the original text for unknown statuses
	return status
}

// nerdFontStatusWidth returns the visible width of a nerdfont status string,
// i.e. the number of icon characters that statusToNerdFont will produce.
func nerdFontStatusWidth(status string) int {
	return width(statusToNerdFont(status))
}

// Column represents a displayable column
type Column string

const (
	ColStatus        Column = "status"
	ColDiff          Column = "diff"
	ColFilename      Column = "filename"
	ColShorthash     Column = "shorthash"
	ColHash          Column = "hash"
	ColDate          Column = "date"
	ColAuthor        Column = "author"
	ColEmail         Column = "email"
	ColNumstat       Column = "numstat"
	ColCommitMessage Column = "commitmessage"
)

// AllColumns returns the default column order
func AllColumns() []Column {
	return []Column{
		ColStatus,
		ColDiff,
		ColFilename,
		ColShorthash,
		ColDate,
		ColAuthor,
		ColCommitMessage,
	}
}

// ValidColumns returns a map of all valid column names
func ValidColumns() map[string]Column {
	return map[string]Column{
		"status":        ColStatus,
		"diff":          ColDiff,
		"filename":      ColFilename,
		"shorthash":     ColShorthash,
		"hash":          ColHash,
		"date":          ColDate,
		"author":        ColAuthor,
		"email":         ColEmail,
		"numstat":       ColNumstat,
		"commitmessage": ColCommitMessage,
	}
}

// calculateColumnWidths computes the maximum width needed for each column
func calculateColumnWidths(files []*File, columns []Column, rctx *RenderContext) map[Column]int {
	widths := make(map[Column]int)

	for _, col := range columns {
		maxWidth := 0
		for _, file := range files {
			var w int
			switch col {
			case ColStatus:
				if rctx.NerdFont {
					w = nerdFontStatusWidth(file.status)
				} else {
					w = width(file.status)
				}
			case ColDiff:
				w = width(file.diffStat)
			case ColFilename:
				w = width(file.Name())
			case ColShorthash:
				w = width(file.shortHash)
			case ColHash:
				w = width(file.hash)
			case ColDate:
				w = width(file.lastModified)
			case ColAuthor:
				w = width(file.author)
			case ColEmail:
				w = width(file.authorEmail)
			case ColNumstat:
				if file.diffSum != nil {
					w = width(fmt.Sprintf("+%d/-%d", file.diffSum.plus, file.diffSum.minus))
				}
			case ColCommitMessage:
				w = width(file.message)
			}
			if w > maxWidth {
				maxWidth = w
			}
		}
		widths[col] = maxWidth
	}

	return widths
}

// renderStatus renders the status column
func renderStatus(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	if maxWidth > 0 {
		var statusStr string
		if rctx.NerdFont {
			statusStr = statusToNerdFont(file.status)
		} else {
			statusStr = file.status
		}
		visibleWidth := width(statusStr)
		// Right-align: pad with spaces on the left
		for i := 0; i < maxWidth-visibleWidth; i++ {
			must(fmt.Fprintf(out, " "))
		}
		must(fmt.Fprintf(out, "%s", statusStr))
	}
}

// renderDiff renders the diff graph column
func renderDiff(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	if maxWidth > 0 {
		must(fmt.Fprintf(out, "%s", file.diffStat))
		for i := 0; i < maxWidth-width(file.diffStat); i++ {
			must(fmt.Fprintf(out, " "))
		}
	}
}

// renderFilename renders the filename column with colors and hyperlinks
func renderFilename(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	if file.isDeleted {
		must(fmt.Fprintf(out, "%s%s", RED, STRIKEOUT))
	} else if file.isDir {
		must(fmt.Fprintf(out, "%s", BLUE))
	} else if file.isExe {
		must(fmt.Fprintf(out, "%s", GREEN))
	}

	// link the file name to the file's location (but not for deleted files)
	if file.isDeleted {
		must(fmt.Fprintf(out, "%s", file.Name()))
	} else {
		fileURL := fmt.Sprintf("file://%s%s", must(os.Hostname()), filepath.Join(rctx.Dir, file.Name()))
		must(fmt.Fprintf(out, "%s", link(fileURL, file.Name())))
	}

	// pad spaces to the right up to maxWidth
	for i := 0; i < maxWidth-width(file.Name()); i++ {
		must(fmt.Fprintf(out, " "))
	}

	// reset color for dir/exe but not deleted (strikethrough continues)
	if file.isDir || file.isExe {
		must(fmt.Fprintf(out, "%s", RESET))
	}
}

// renderShorthash renders the short commit hash
func renderShorthash(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	if maxWidth > 0 {
		shortHash := file.shortHash
		shortHashWidth := min(width(shortHash), maxWidth)
		var color string
		if rctx.MonoHash {
			color = CYAN
		} else {
			color = hashToColor(file.hash)
		}

		if len(rctx.GithubURL) > 0 && shortHash != "" {
			commitURL := fmt.Sprintf("%s/commit/%s", rctx.GithubURL, file.hash)
			must(fmt.Fprintf(out, "%s%s%s", color, link(commitURL, shortHash[:min(len(shortHash), maxWidth)]), RESET))
		} else {
			must(fmt.Fprintf(out, "%s%s%s", color, shortHash[:min(len(shortHash), maxWidth)], RESET))
		}

		// pad spaces to the right up to maxWidth
		for i := 0; i < maxWidth-shortHashWidth; i++ {
			must(fmt.Fprintf(out, " "))
		}
	}
}

// renderHash renders the full commit hash
func renderHash(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	if maxWidth > 0 {
		var color string
		if rctx.MonoHash {
			color = CYAN
		} else {
			color = hashToColor(file.hash)
		}
		if len(rctx.GithubURL) > 0 && file.hash != "" {
			commitURL := fmt.Sprintf("%s/commit/%s", rctx.GithubURL, file.hash)
			must(fmt.Fprintf(out, "%s%s%s", color, link(commitURL, file.hash), RESET))
		} else {
			must(fmt.Fprintf(out, "%s%s%s", color, file.hash, RESET))
		}

		// pad spaces to the right up to maxWidth
		for i := 0; i < maxWidth-width(file.hash); i++ {
			must(fmt.Fprintf(out, " "))
		}
	}
}

// renderDate renders the date column
func renderDate(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	must(fmt.Fprintf(out, "%s", file.lastModified))
	// pad spaces to the right up to maxWidth
	for i := 0; i < maxWidth-width(file.lastModified); i++ {
		must(fmt.Fprintf(out, " "))
	}
}

// renderAuthor renders the author column with hyperlinks
func renderAuthor(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	authorWidth := min(width(file.author), maxWidth)

	// Truncate author string if needed, being careful with multi-byte characters
	truncatedAuthor := truncateToWidth(file.author, authorWidth)

	if len(rctx.GithubURL) > 0 {
		authorLink := fmt.Sprintf("%s/commits?author=%s", rctx.GithubURL, file.authorEmail)
		if file.isDeleted {
			must(fmt.Fprintf(out, "%s", link(authorLink, truncatedAuthor)))
		} else {
			must(fmt.Fprintf(out, "%s%s%s", YELLOW, link(authorLink, truncatedAuthor), RESET))
		}
	} else {
		if file.isDeleted {
			must(fmt.Fprintf(out, "%s", truncatedAuthor))
		} else {
			must(fmt.Fprintf(out, "%s%s%s", YELLOW, truncatedAuthor, RESET))
		}
	}

	// pad spaces to the right up to maxWidth
	for i := 0; i < maxWidth-width(truncatedAuthor); i++ {
		must(fmt.Fprintf(out, " "))
	}
}

// renderEmail renders the author email column
func renderEmail(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	// Calculate how many characters we can display
	emailWidth := min(width(file.authorEmail), maxWidth)

	// Truncate email string if needed
	truncatedEmail := truncateToWidth(file.authorEmail, emailWidth)
	must(fmt.Fprintf(out, "%s", truncatedEmail))

	// pad spaces to the right up to maxWidth
	for i := 0; i < maxWidth-width(truncatedEmail); i++ {
		must(fmt.Fprintf(out, " "))
	}
}

// renderNumstat renders the numeric diffstat
func renderNumstat(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	if file.diffSum != nil {
		numstat := fmt.Sprintf("+%d/-%d", file.diffSum.plus, file.diffSum.minus)
		numstatWidth := min(width(numstat), maxWidth)

		truncatedNumstat := truncateToWidth(numstat, numstatWidth)
		must(fmt.Fprintf(out, "%s", truncatedNumstat))

		// pad spaces to the right up to maxWidth
		for i := 0; i < maxWidth-width(truncatedNumstat); i++ {
			must(fmt.Fprintf(out, " "))
		}
	} else {
		// pad spaces if no diffSum
		for range maxWidth {
			must(fmt.Fprintf(out, " "))
		}
	}
}

// renderCommitMessage renders the commit message with issue linkification
func renderCommitMessage(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	// Calculate how many characters we can display
	messageWidth := min(width(file.message), maxWidth)

	// Truncate message string if needed
	truncatedMessage := truncateToWidth(file.message, messageWidth)

	if file.isDeleted {
		if len(rctx.GithubURL) > 0 {
			must(fmt.Fprintf(out, "%s", linkify(truncatedMessage, rctx.GithubURL, file.hash)))
		} else {
			must(fmt.Fprintf(out, "%s", truncatedMessage))
		}
	} else if len(rctx.GithubURL) > 0 {
		must(fmt.Fprintf(out, "%s", linkify(truncatedMessage, rctx.GithubURL, file.hash)))
	} else {
		must(fmt.Fprintf(out, "%s", truncatedMessage))
	}

	// pad spaces to the right up to maxWidth
	for i := 0; i < maxWidth-width(truncatedMessage); i++ {
		must(fmt.Fprintf(out, " "))
	}
}

// getColumnRenderer returns the renderer function for a column
func getColumnRenderer(col Column) func(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	switch col {
	case ColStatus:
		return renderStatus
	case ColDiff:
		return renderDiff
	case ColFilename:
		return renderFilename
	case ColShorthash:
		return renderShorthash
	case ColHash:
		return renderHash
	case ColDate:
		return renderDate
	case ColAuthor:
		return renderAuthor
	case ColEmail:
		return renderEmail
	case ColNumstat:
		return renderNumstat
	case ColCommitMessage:
		return renderCommitMessage
	default:
		return func(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {}
	}
}
