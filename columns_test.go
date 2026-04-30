package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestRenderDiffMaxWidth tests that renderDiff respects maxWidth exactly
func TestRenderDiffMaxWidth(t *testing.T) {
	tests := []struct {
		name      string
		diffStat  string
		maxWidth  int
		wantWidth int
	}{
		{
			name:      "diff shorter than maxWidth",
			diffStat:  "++++",
			maxWidth:  10,
			wantWidth: 10,
		},
		{
			name:      "diff equal to maxWidth",
			diffStat:  "==========",
			maxWidth:  10,
			wantWidth: 10,
		},
		{
			name:      "diff with color codes",
			diffStat:  GREEN + "+++" + RED + "---" + RESET,
			maxWidth:  20,
			wantWidth: 20,
		},
		{
			name:      "minimal width",
			diffStat:  "+",
			maxWidth:  5,
			wantWidth: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := &File{
				diffStat: tt.diffStat,
			}
			var buf bytes.Buffer
			rctx := &RenderContext{}

			renderDiff(&buf, file, tt.maxWidth, rctx, false)

			output := buf.String()
			// Strip ANSI codes to measure actual visible width
			visibleOutput := stripANSI(output)
			actualWidth := len(visibleOutput)

			if actualWidth != tt.wantWidth {
				t.Errorf("renderDiff() output width = %d, want %d\nRaw output: %q\nVisible output: %q",
					actualWidth, tt.wantWidth, output, visibleOutput)
			}
		})
	}
}

// TestShowColumnsMaxWidth tests that showColumns respects terminal width
func TestShowColumnsMaxWidth(t *testing.T) {
	tests := []struct {
		name     string
		maxWidth int
		files    []*File
		columns  []Column
	}{
		{
			name:     "single column respects maxWidth",
			maxWidth: 20,
			files: []*File{
				{
					entry:    &mockDirEntry{name: "file.go"},
					diffStat: GREEN + "++++" + RED + "----" + RESET,
					status:   "M ",
				},
			},
			columns: []Column{ColStatus, ColDiff, ColFilename},
		},
		{
			name:     "multiple files respect maxWidth",
			maxWidth: 50,
			files: []*File{
				{
					entry:    &mockDirEntry{name: "file1.go"},
					diffStat: GREEN + "+++" + RESET,
					status:   "M ",
				},
				{
					entry:    &mockDirEntry{name: "file2.go"},
					diffStat: RED + "---" + RESET,
					status:   "A ",
				},
			},
			columns: []Column{ColStatus, ColDiff, ColFilename},
		},
		{
			name:     "all columns with tight width",
			maxWidth: 80,
			files: []*File{
				{
					entry:        &mockDirEntry{name: "main.go"},
					diffStat:     GREEN + "++++++++" + RED + "--------" + RESET,
					status:       "M ",
					shortHash:    "abc123",
					hash:         "abc123def456",
					lastModified: "2024-01-01",
					author:       "John Doe",
					authorEmail:  "john@example.com",
					message:      "Fix bug in main function",
				},
			},
			columns: AllColumns(),
		},
		{
			name:     "default layout at 93 columns - regression test for off-by-one",
			maxWidth: 93,
			files: []*File{
				{
					entry:        &mockDirEntry{name: "Makefile"},
					diffStat:     "    ",
					status:       "  ",
					shortHash:    "dd59602",
					hash:         "dd59602e5a8af4da0429237c2f2d547ee1bdc800",
					lastModified: "2026-03-31",
					author:       "Bill Mill",
					authorEmail:  "bill@example.com",
					message:      "feat: add --format flag for customizable columns",
				},
				{
					entry:        &mockDirEntry{name: "AGENTS.md"},
					diffStat:     "    ",
					status:       "  ",
					shortHash:    "dd59602",
					hash:         "dd59602e5a8af4da0429237c2f2d547ee1bdc800",
					lastModified: "2026-03-31",
					author:       "Bill Mill",
					authorEmail:  "bill@example.com",
					message:      "feat: add --format flag for customizable columns",
				},
			},
			columns: AllColumns(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			rctx := &RenderContext{
				Dir: "/test",
			}

			showColumns(&buf, tt.maxWidth, tt.files, rctx, tt.columns)

			// Check each line doesn't exceed maxWidth
			lines := strings.Split(buf.String(), "\n")
			for i, line := range lines {
				if line == "" {
					continue // skip empty lines
				}
				// Strip ANSI codes to measure visible width
				visibleLine := stripANSI(line)
				actualWidth := len(visibleLine)

				if actualWidth > tt.maxWidth {
					t.Errorf("Line %d exceeds maxWidth: got %d chars, want <= %d\nRaw line: %q\nVisible line: %q",
						i+1, actualWidth, tt.maxWidth, line, visibleLine)
				}
			}
		})
	}
}

// TestCalculateColumnWidths tests that column width calculation is accurate
func TestCalculateColumnWidths(t *testing.T) {
	files := []*File{
		{
			entry:        &mockDirEntry{name: "short.go"},
			status:       "M ",
			diffStat:     "+++",
			shortHash:    "abc123",
			hash:         "abc123def456789",
			lastModified: "2024-01-01",
			author:       "Alice",
			authorEmail:  "alice@example.com",
			message:      "Short message",
			diffSum:      &Diff{plus: 10, minus: 5},
		},
		{
			entry:        &mockDirEntry{name: "verylongfilename.go"},
			status:       "A ",
			diffStat:     "++++++++++",
			shortHash:    "def456",
			hash:         "def456abc123xyz",
			lastModified: "2024-12-31",
			author:       "Robert Johnson",
			authorEmail:  "robert.johnson@example.com",
			message:      "This is a much longer commit message",
			diffSum:      &Diff{plus: 100, minus: 50},
		},
	}

	columns := []Column{ColStatus, ColDiff, ColFilename, ColShorthash, ColDate, ColAuthor, ColCommitMessage}
	rctx := &RenderContext{}
	widths := calculateColumnWidths(files, columns, rctx)

	// Verify widths match the longest content in each column
	expectedWidths := map[Column]int{
		ColStatus:        2, // "M " or "A "
		ColDiff:          10,
		ColFilename:      19, // "verylongfilename.go"
		ColShorthash:     6,  // "def456"
		ColDate:          10, // "2024-12-31"
		ColAuthor:        14, // "Robert Johnson"
		ColCommitMessage: 36, // "This is a much longer commit message"
	}

	for col, expected := range expectedWidths {
		if widths[col] != expected {
			t.Errorf("Column %s width = %d, want %d", col, widths[col], expected)
		}
	}
}

// TestStatusToNerdFont tests the nerdfont status mapping.
// The index (staged) character is green, the worktree (unstaged) character is red.
func TestStatusToNerdFont(t *testing.T) {
	greenIcon := func(icon string) string { return GREEN + icon + RESET }
	redIcon := func(icon string) string { return RED + icon + RESET }

	pencil := "\uf040"
	diffAdded := "\uf471"
	trash := "\uf014"
	rename := "\uf064"
	copy := "\uf0c5"
	bolt := "\uf0e7"
	question := "\uf128"

	tests := []struct {
		name     string
		status   string
		expected string
	}{
		{name: "empty status", status: "", expected: ""},
		{name: "staged modification", status: "M ", expected: greenIcon(pencil) + " "},
		{name: "unstaged modification", status: " M", expected: redIcon(pencil) + " "},
		{name: "both modified", status: "MM", expected: greenIcon(pencil) + " " + redIcon(pencil) + " "},
		{name: "staged addition", status: "A ", expected: greenIcon(diffAdded) + " "},
		{name: "added then modified", status: "AM", expected: greenIcon(diffAdded) + " " + redIcon(pencil) + " "},
		{name: "staged deletion", status: "D ", expected: greenIcon(trash) + " "},
		{name: "unstaged deletion", status: " D", expected: redIcon(trash) + " "},
		{name: "untracked", status: "??", expected: question + " "},
		{name: "ignored", status: "I", expected: "\uf070 "},
		{name: "renamed", status: "R ", expected: greenIcon(rename) + " "},
		{name: "renamed then modified", status: "RM", expected: greenIcon(rename) + " " + redIcon(pencil) + " "},
		{name: "copied", status: "C ", expected: greenIcon(copy) + " "},
		{name: "unmerged both modified", status: "UU", expected: bolt + " "},
		{name: "unmerged both added", status: "AA", expected: bolt + " "},
		{name: ".git directory", status: "*", expected: "\ue5fb "},
		{name: "unknown status falls back", status: "ZZ", expected: "ZZ"},
		{
			name:     "compound status",
			status:   "M , D",
			expected: greenIcon(pencil) + " " + redIcon(trash) + " ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := statusToNerdFont(tt.status)
			if result != tt.expected {
				t.Errorf("statusToNerdFont(%q) = %q, want %q", tt.status, result, tt.expected)
			}
		})
	}
}

// TestStagedVsUnstagedDistinct verifies that staged and unstaged versions of
// the same action produce different output (different colors).
func TestStagedVsUnstagedDistinct(t *testing.T) {
	pairs := []struct{ staged, unstaged string }{
		{"M ", " M"},
		{"D ", " D"},
	}
	for _, p := range pairs {
		stagedResult := statusToNerdFont(p.staged)
		unstagedResult := statusToNerdFont(p.unstaged)
		if stagedResult == unstagedResult {
			t.Errorf("staged %q and unstaged %q should produce different output, both got %q",
				p.staged, p.unstaged, stagedResult)
		}
	}
}

// TestRenderStatusNerdFont tests that renderStatus uses nerdfont icons when enabled
func TestRenderStatusNerdFont(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		nerdFont bool
		maxWidth int
	}{
		{
			name:     "nerdfont off shows raw status",
			status:   "M ",
			nerdFont: false,
			maxWidth: 4,
		},
		{
			name:     "nerdfont on shows icon",
			status:   "M ",
			nerdFont: true,
			maxWidth: 4,
		},
		{
			name:     "nerdfont untracked",
			status:   "??",
			nerdFont: true,
			maxWidth: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := &File{status: tt.status}
			var buf bytes.Buffer
			rctx := &RenderContext{NerdFont: tt.nerdFont}

			renderStatus(&buf, file, tt.maxWidth, rctx, false)

			output := buf.String()
			visibleOutput := stripANSI(output)
			actualWidth := width(visibleOutput)

			// Width should not exceed maxWidth
			if actualWidth > tt.maxWidth {
				t.Errorf("renderStatus() width = %d, want <= %d\nOutput: %q", actualWidth, tt.maxWidth, visibleOutput)
			}

			if tt.nerdFont {
				// The visible output (stripped of ANSI) should contain nerdfont icons
				stripped := stripANSI(output)
				expectedStripped := stripANSI(statusToNerdFont(tt.status))
				if !strings.Contains(stripped, expectedStripped) {
					t.Errorf("renderStatus() with nerdfont: visible output %q should contain %q", stripped, expectedStripped)
				}
				// Should NOT contain the raw letter status
				if strings.Contains(stripped, tt.status) && tt.status != "" {
					t.Errorf("renderStatus() with nerdfont should not contain raw status %q in visible output", tt.status)
				}
			} else {
				// Should contain the raw status text
				if !strings.Contains(output, tt.status) {
					t.Errorf("renderStatus() without nerdfont should contain %q, got %q", tt.status, output)
				}
			}
		})
	}
}

// TestCalculateColumnWidthsNerdFont tests that column width calculation works with nerdfont
func TestCalculateColumnWidthsNerdFont(t *testing.T) {
	files := []*File{
		{
			entry:  &mockDirEntry{name: "file1.go"},
			status: "M ", // staged only → 1 icon
		},
		{
			entry:  &mockDirEntry{name: "file2.go"},
			status: "??", // both positions → 2 icons
		},
	}

	// Without nerdfont, status width should be 2 ("M " and "??" are both 2 chars)
	rctxOff := &RenderContext{NerdFont: false}
	widthsOff := calculateColumnWidths(files, []Column{ColStatus}, rctxOff)
	if widthsOff[ColStatus] != 2 {
		t.Errorf("Without nerdfont, status width = %d, want 2", widthsOff[ColStatus])
	}

	// With nerdfont, both "M " and "??" produce icon+space (width 2)
	rctxOn := &RenderContext{NerdFont: true}
	widthsOn := calculateColumnWidths(files, []Column{ColStatus}, rctxOn)
	if widthsOn[ColStatus] != 2 {
		t.Errorf("With nerdfont, status width = %d, want 2", widthsOn[ColStatus])
	}

	// With only single-position statuses, width should be 2 (icon + trailing space)
	singleFiles := []*File{
		{entry: &mockDirEntry{name: "a.go"}, status: "M "},
		{entry: &mockDirEntry{name: "b.go"}, status: " D"},
	}
	widthsSingle := calculateColumnWidths(singleFiles, []Column{ColStatus}, rctxOn)
	if widthsSingle[ColStatus] != 2 {
		t.Errorf("With nerdfont single-position statuses, width = %d, want 2", widthsSingle[ColStatus])
	}

	// With MM (both positions), width should be 4 (icon + space + icon + trailing space)
	bothFiles := []*File{
		{entry: &mockDirEntry{name: "a.go"}, status: "M "},
		{entry: &mockDirEntry{name: "b.go"}, status: "MM"},
	}
	widthsBoth := calculateColumnWidths(bothFiles, []Column{ColStatus}, rctxOn)
	if widthsBoth[ColStatus] != 4 {
		t.Errorf("With nerdfont MM status, width = %d, want 4", widthsBoth[ColStatus])
	}
}

// TestTerminalWidthBug reproduces the issue where lines become blank when the
// terminal width falls exactly at the end of the author name
func TestTerminalWidthBug(t *testing.T) {
	// Create a file with Christina Ahrens Roberts as the author
	files := []*File{
		{
			entry:        &mockDirEntry{name: "test.py"},
			status:       " M",
			diffStat:     "++--",
			shortHash:    "7863cc2b54",
			hash:         "7863cc2b54abc123def456",
			lastModified: "2026-03-26",
			author:       "Christina Ahrens Roberts",
			message:      "Update tests",
		},
	}

	columns := AllColumns()
	rctx := &RenderContext{
		Dir: "/test",
	}

	// Calculate what the normal column widths would be
	colWidths := calculateColumnWidths(files, columns, rctx)

	// Calculate the cumulative widths up to and including the author column
	cumulativeWidth := 0
	for i, col := range columns {
		if i > 0 {
			cumulativeWidth += 1 // space between columns
		}
		cumulativeWidth += colWidths[col]

		if col == ColAuthor {
			t.Logf("Width up to end of author column: %d", cumulativeWidth)
			break
		}
	}

	// Test with terminal widths around the author column boundary
	testWidths := []int{
		cumulativeWidth - 5, // Middle of author name
		cumulativeWidth - 1, // Just before end of author name
		cumulativeWidth,     // Exactly at end of author name
		cumulativeWidth + 1, // One character after author name
	}

	for _, maxWidth := range testWidths {
		t.Run(fmt.Sprintf("width_%d", maxWidth), func(t *testing.T) {
			var buf bytes.Buffer
			showColumns(&buf, maxWidth, files, rctx, columns)

			output := buf.String()
			lines := strings.Split(output, "\n")

			for i, line := range lines {
				if line == "" {
					continue
				}

				// Strip ANSI codes to measure visible width
				visibleLine := stripANSI(line)
				actualWidth := len(visibleLine)

				t.Logf("Terminal width %d: Line %d visible width = %d", maxWidth, i+1, actualWidth)
				t.Logf("  Raw: %q", line)
				t.Logf("  Visible: %q", visibleLine)

				// The line should not be empty (all whitespace)
				if strings.TrimSpace(visibleLine) == "" {
					t.Errorf("Line %d is blank at terminal width %d", i+1, maxWidth)
				}

				// The line should not exceed maxWidth
				if actualWidth > maxWidth {
					t.Errorf("Line %d exceeds maxWidth: got %d chars, want <= %d", i+1, actualWidth, maxWidth)
				}
			}
		})
	}
}

// TestCalculateColumnWidthsWithDiacritics tests that diacritics are handled correctly
func TestCalculateColumnWidthsWithDiacritics(t *testing.T) {
	files := []*File{
		{
			entry:        &mockDirEntry{name: "file1.go"},
			status:       "M ",
			diffStat:     "+++",
			shortHash:    "abc123",
			hash:         "abc123def456789",
			lastModified: "2024-01-01",
			author:       "Pål Grønås Drange", // 17 characters, but visually 17 columns
			authorEmail:  "pal@example.com",
			message:      "Add café support", // 16 characters with é
		},
		{
			entry:        &mockDirEntry{name: "file2.go"},
			status:       "A ",
			diffStat:     "++++",
			shortHash:    "def456",
			hash:         "def456abc123xyz",
			lastModified: "2024-12-31",
			author:       "Miro Hrončok", // 12 characters with č
			authorEmail:  "miro@example.com",
			message:      "Fix naïve algorithm", // 19 characters with ï
		},
	}

	columns := []Column{ColAuthor, ColCommitMessage}
	rctx := &RenderContext{}
	widths := calculateColumnWidths(files, columns, rctx)

	// The author column should use the visual width (all characters are single-width)
	// "Pål Grønås Drange" is 17 characters and 17 visual columns
	if widths[ColAuthor] != 17 {
		t.Errorf("Author column width = %d, want 17 for 'Pål Grønås Drange'", widths[ColAuthor])
	}

	// "Fix naïve algorithm" is 19 characters and 19 visual columns
	if widths[ColCommitMessage] != 19 {
		t.Errorf("Message column width = %d, want 19 for 'Fix naïve algorithm'", widths[ColCommitMessage])
	}
}
