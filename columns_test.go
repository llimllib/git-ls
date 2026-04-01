package main

import (
	"bytes"
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

			renderDiff(&buf, file, tt.maxWidth, rctx)

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
	widths := calculateColumnWidths(files, columns)

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
	widths := calculateColumnWidths(files, columns)

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
