package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

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
func calculateColumnWidths(files []*File, columns []Column) map[Column]int {
	widths := make(map[Column]int)

	for _, col := range columns {
		maxWidth := 0
		for _, file := range files {
			var w int
			switch col {
			case ColStatus:
				w = len(file.status)
			case ColDiff:
				w = width(file.diffStat)
			case ColFilename:
				w = len(file.Name())
			case ColShorthash:
				w = len(file.shortHash)
			case ColHash:
				w = len(file.hash)
			case ColDate:
				w = len(file.lastModified)
			case ColAuthor:
				w = len(file.author)
			case ColEmail:
				w = len(file.authorEmail)
			case ColNumstat:
				if file.diffSum != nil {
					w = len(fmt.Sprintf("+%d/-%d", file.diffSum.plus, file.diffSum.minus))
				}
			case ColCommitMessage:
				w = len(file.message)
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
		must(fmt.Fprintf(out, fmt.Sprintf("%%%ds", maxWidth), file.status))
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
	for i := 0; i < maxWidth-len(file.Name()); i++ {
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
		shortHashWidth := min(len(shortHash), maxWidth)
		var color string
		if rctx.MonoHash {
			color = CYAN
		} else {
			color = hashToColor(file.hash)
		}

		if len(rctx.GithubURL) > 0 && shortHash != "" {
			commitURL := fmt.Sprintf("%s/commit/%s", rctx.GithubURL, file.hash)
			must(fmt.Fprintf(out, "%s%s%s", color, link(commitURL, shortHash[:shortHashWidth]), RESET))
		} else {
			must(fmt.Fprintf(out, "%s%s%s", color, shortHash[:shortHashWidth], RESET))
		}

		// pad spaces to the right up to maxWidth
		for i := 0; i < maxWidth-len(shortHash); i++ {
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
		for i := 0; i < maxWidth-len(file.hash); i++ {
			must(fmt.Fprintf(out, " "))
		}
	}
}

// renderDate renders the date column
func renderDate(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	must(fmt.Fprintf(out, "%s", file.lastModified))
	// pad spaces to the right up to maxWidth
	for i := 0; i < maxWidth-len(file.lastModified); i++ {
		must(fmt.Fprintf(out, " "))
	}
}

// renderAuthor renders the author column with hyperlinks
func renderAuthor(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	authorWidth := min(len(file.author), maxWidth)
	if len(rctx.GithubURL) > 0 {
		authorLink := fmt.Sprintf("%s/commits?author=%s", rctx.GithubURL, file.authorEmail)
		if file.isDeleted {
			must(fmt.Fprintf(out, "%s", link(authorLink, file.author[:authorWidth])))
		} else {
			must(fmt.Fprintf(out, "%s%s%s", YELLOW, link(authorLink, file.author[:authorWidth]), RESET))
		}
	} else {
		if file.isDeleted {
			must(fmt.Fprintf(out, "%s", file.author[:authorWidth]))
		} else {
			must(fmt.Fprintf(out, "%s%s%s", YELLOW, file.author[:authorWidth], RESET))
		}
	}

	// pad spaces to the right up to maxWidth
	for i := 0; i < maxWidth-authorWidth; i++ {
		must(fmt.Fprintf(out, " "))
	}
}

// renderEmail renders the author email column
func renderEmail(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	emailWidth := min(len(file.authorEmail), maxWidth)
	must(fmt.Fprintf(out, "%s", file.authorEmail[:emailWidth]))

	// pad spaces to the right up to maxWidth
	for i := 0; i < maxWidth-emailWidth; i++ {
		must(fmt.Fprintf(out, " "))
	}
}

// renderNumstat renders the numeric diffstat
func renderNumstat(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	if file.diffSum != nil {
		numstat := fmt.Sprintf("+%d/-%d", file.diffSum.plus, file.diffSum.minus)
		numstatWidth := min(len(numstat), maxWidth)
		must(fmt.Fprintf(out, "%s", numstat[:numstatWidth]))

		// pad spaces to the right up to maxWidth
		for i := 0; i < maxWidth-numstatWidth; i++ {
			must(fmt.Fprintf(out, " "))
		}
	} else {
		// pad spaces if no diffSum
		for i := 0; i < maxWidth; i++ {
			must(fmt.Fprintf(out, " "))
		}
	}
}

// renderCommitMessage renders the commit message with issue linkification
func renderCommitMessage(out io.Writer, file *File, maxWidth int, rctx *RenderContext) {
	messageWidth := min(len(file.message), maxWidth)
	if file.isDeleted {
		if len(rctx.GithubURL) > 0 {
			must(fmt.Fprintf(out, "%s", linkify(file.message[:messageWidth], rctx.GithubURL, file.hash)))
		} else {
			must(fmt.Fprintf(out, "%s", file.message[:messageWidth]))
		}
	} else if len(rctx.GithubURL) > 0 {
		must(fmt.Fprintf(out, "%s", linkify(file.message[:messageWidth], rctx.GithubURL, file.hash)))
	} else {
		must(fmt.Fprintf(out, "%s", file.message[:messageWidth]))
	}

	// pad spaces to the right up to maxWidth
	for i := 0; i < maxWidth-messageWidth; i++ {
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
