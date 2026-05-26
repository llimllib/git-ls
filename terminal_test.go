package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestSkipANSI(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		i        int
		expected int
	}{
		{
			name:     "no escape at position",
			s:        "hello",
			i:        0,
			expected: 0,
		},
		{
			name:     "CSI color code",
			s:        "\x1b[32mgreen",
			i:        0,
			expected: 5, // \x1b [ 3 2 m
		},
		{
			name:     "CSI reset",
			s:        "\x1b[0m",
			i:        0,
			expected: 4,
		},
		{
			name:     "CSI 8-bit color",
			s:        "\x1b[38;5;123mtext",
			i:        0,
			expected: 11,
		},
		{
			name:     "OSC hyperlink",
			s:        "\x1b]8;;https://example.com\x1b\\link text\x1b]8;;\x1b\\",
			i:        0,
			expected: 26, // \x1b]8;;https://example.com\x1b\\
		},
		{
			name:     "escape in middle of string",
			s:        "hi \x1b[31mred",
			i:        3,
			expected: 5,
		},
		{
			name:     "not at escape position",
			s:        "hi \x1b[31mred",
			i:        1,
			expected: 0,
		},
		{
			name:     "at end of string",
			s:        "hi",
			i:        1,
			expected: 0,
		},
		{
			name:     "strikeout",
			s:        "\x1b[9mstrikethrough",
			i:        0,
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := skipANSI(tt.s, tt.i)
			if got != tt.expected {
				t.Errorf("skipANSI(%q, %d) = %d, want %d", tt.s, tt.i, got, tt.expected)
			}
		})
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no escapes",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "single color",
			input:    "\x1b[32mgreen\x1b[0m",
			expected: "green",
		},
		{
			name:     "multiple colors",
			input:    "\x1b[31mred\x1b[0m and \x1b[34mblue\x1b[0m",
			expected: "red and blue",
		},
		{
			name:     "hyperlink",
			input:    "\x1b]8;;https://example.com\x1b\\click here\x1b]8;;\x1b\\",
			expected: "click here",
		},
		{
			name:     "color inside hyperlink",
			input:    "\x1b[36m\x1b]8;;https://x.com\x1b\\text\x1b]8;;\x1b\\\x1b[0m",
			expected: "text",
		},
		{
			name:     "8-bit color",
			input:    "\x1b[38;5;42mcolored\x1b[0m",
			expected: "colored",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "strikeout and color",
			input:    "\x1b[31m\x1b[9mdeleted\x1b[0m",
			expected: "deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(tt.input)
			if got != tt.expected {
				t.Errorf("stripANSI(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestWidth(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "plain ASCII",
			input:    "hello",
			expected: 5,
		},
		{
			name:     "with color codes",
			input:    "\x1b[32mgreen\x1b[0m",
			expected: 5,
		},
		{
			name:     "with hyperlink",
			input:    "\x1b]8;;https://example.com\x1b\\click\x1b]8;;\x1b\\",
			expected: 5,
		},
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "wide characters (CJK)",
			input:    "日本語",
			expected: 6,
		},
		{
			name:     "mixed ASCII and wide",
			input:    "hi日本",
			expected: 6,
		},
		{
			name:     "colored wide characters",
			input:    "\x1b[31m日本\x1b[0m",
			expected: 4,
		},
		{
			name:     "diff graph with colors",
			input:    fmt.Sprintf("%s++%s--%s", GREEN, RED, RESET),
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := width(tt.input)
			if got != tt.expected {
				t.Errorf("width(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTruncateToWidth(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
		expected string // expected visible text (after stripping ANSI)
	}{
		{
			name:     "no truncation needed",
			input:    "hello",
			maxWidth: 10,
			expected: "hello",
		},
		{
			name:     "truncate plain text",
			input:    "hello world",
			maxWidth: 5,
			expected: "hello",
		},
		{
			name:     "truncate at zero",
			input:    "hello",
			maxWidth: 0,
			expected: "",
		},
		{
			name:     "preserve ANSI color when truncating",
			input:    "\x1b[32mgreen text\x1b[0m",
			maxWidth: 5,
			expected: "green",
		},
		{
			name:     "ANSI codes don't count toward width",
			input:    "\x1b[32mhi\x1b[0m",
			maxWidth: 2,
			expected: "hi",
		},
		{
			name:     "hyperlink preserved when text fits",
			input:    "\x1b]8;;https://example.com\x1b\\hey\x1b]8;;\x1b\\",
			maxWidth: 3,
			expected: "hey",
		},
		{
			name:     "hyperlink text truncated",
			input:    "\x1b]8;;https://example.com\x1b\\hello\x1b]8;;\x1b\\",
			maxWidth: 3,
			expected: "hel",
		},
		{
			name:     "wide characters truncated at boundary",
			input:    "日本語",
			maxWidth: 4,
			expected: "日本",
		},
		{
			name:     "wide char doesn't fit partially",
			input:    "日本語",
			maxWidth: 3,
			expected: "日", // Can't fit half of 本
		},
		{
			name:     "colored diff graph truncated",
			input:    fmt.Sprintf("%s+++%s---%s", GREEN, RED, RESET),
			maxWidth: 4,
			expected: "+++-",
		},
		{
			name:     "date string no truncation",
			input:    "2024-10-01",
			maxWidth: 10,
			expected: "2024-10-01",
		},
		{
			name:     "date string truncated by 1",
			input:    "2024-10-01",
			maxWidth: 9,
			expected: "2024-10-0",
		},
		{
			name:     "nerdfont icon with color preserved",
			input:    "\x1b[32m\uf040\x1b[0m ",
			maxWidth: 2,
			expected: "\uf040 ",
		},
		{
			name:     "nerdfont icon with color truncated to 1",
			input:    "\x1b[32m\uf040\x1b[0m ",
			maxWidth: 1,
			expected: "\uf040",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToWidth(tt.input, tt.maxWidth)
			gotVisible := stripANSI(got)

			if gotVisible != tt.expected {
				t.Errorf("truncateToWidth(%q, %d) visible = %q, want %q\n  raw output: %q",
					tt.input, tt.maxWidth, gotVisible, tt.expected, got)
			}

			// Verify width invariant: visible width must not exceed maxWidth
			gotWidth := width(got)
			if gotWidth > tt.maxWidth {
				t.Errorf("truncateToWidth(%q, %d) produced width %d, exceeds max",
					tt.input, tt.maxWidth, gotWidth)
			}
		})
	}
}

func TestTruncateToWidthPreservesANSI(t *testing.T) {
	// Verify that ANSI codes pass through (not stripped) when text fits.
	// Trailing ANSI codes (like RESET) after the last visible character must
	// be preserved to avoid color bleeding into subsequent output.
	input := "\x1b[32mgreen\x1b[0m"
	got := truncateToWidth(input, 5)
	gotVisible := stripANSI(got)
	if gotVisible != "green" {
		t.Errorf("truncateToWidth visible text = %q, want %q", gotVisible, "green")
	}
	if !strings.Contains(got, "\x1b[32m") {
		t.Errorf("truncateToWidth should preserve leading ANSI: %q", got)
	}
	// Trailing RESET must be preserved
	if !strings.Contains(got, "\x1b[0m") {
		t.Errorf("truncateToWidth should preserve trailing RESET: %q", got)
	}

	// Verify color code is kept even when text is truncated
	got = truncateToWidth(input, 3)
	if !strings.Contains(got, "\x1b[32m") {
		t.Errorf("truncateToWidth should keep leading ANSI in truncated output: %q", got)
	}
	if width(got) != 3 {
		t.Errorf("truncateToWidth visible width = %d, want 3", width(got))
	}

	// Verify diff graph pattern: GREEN+++RED--RESET doesn't lose RESET
	diffGraph := "\x1b[32m+++\x1b[31m--\x1b[0m"
	got = truncateToWidth(diffGraph, 5)
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("truncateToWidth should preserve trailing RESET in diff graph: %q", got)
	}
	if width(got) != 5 {
		t.Errorf("truncateToWidth diff graph visible width = %d, want 5", width(got))
	}
}

func TestHashToColor(t *testing.T) {
	tests := []struct {
		name string
		hash string
	}{
		{
			name: "empty hash returns cyan",
			hash: "",
		},
		{
			name: "same hash returns same color",
			hash: "abc123",
		},
		{
			name: "different hash",
			hash: "def456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			color := hashToColor(tt.hash)

			// Empty hash should return CYAN
			if tt.hash == "" {
				if color != CYAN {
					t.Errorf("hashToColor(\"\") = %q, expected CYAN (%q)", color, CYAN)
				}
				return
			}

			// Non-empty hash should return a valid 8-bit color code
			if !strings.HasPrefix(color, "\x1b[38;5;") || !strings.HasSuffix(color, "m") {
				t.Errorf("hashToColor(%q) = %q, expected 8-bit color format", tt.hash, color)
			}

			// Same hash should always return same color (deterministic)
			color2 := hashToColor(tt.hash)
			if color != color2 {
				t.Errorf("hashToColor not deterministic: first=%q, second=%q", color, color2)
			}
		})
	}

	// Different hashes should (likely) produce different colors
	color1 := hashToColor("55b998eae87118427808144927549a39e821c0fb")
	color2 := hashToColor("dd59602e5a8af4da0429237c2f2d547ee1bdc800")
	if color1 == color2 {
		t.Errorf("Different hashes produced same color: both got %q", color1)
	}
}

func TestLinkify(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		github   string
		hash     string
		expected string
	}{
		{
			name:     "no issue references",
			msg:      "Some message",
			github:   "https://github.com/a/b",
			hash:     "123abc",
			expected: link("https://github.com/a/b/commit/123abc", "Some message"),
		},
		{
			name:   "one issue reference",
			msg:    "fixes issue (#17)",
			github: "https://github.com/a/b",
			hash:   "123abc",
			expected: link("https://github.com/a/b/commit/123abc", "fixes issue (") +
				link("https://github.com/a/b/pull/17", fmt.Sprintf("%s%s%s", BLUE, "#17", RESET)) +
				link("https://github.com/a/b/commit/123abc", ")"),
		},
		{
			name:   "two issue references",
			msg:    "fixes (#17) closes (#99)",
			github: "https://github.com/a/b",
			hash:   "123abc",
			expected: link("https://github.com/a/b/commit/123abc", "fixes (") +
				link("https://github.com/a/b/pull/17", fmt.Sprintf("%s%s%s", BLUE, "#17", RESET)) +
				link("https://github.com/a/b/commit/123abc", ") closes (") +
				link("https://github.com/a/b/pull/99", fmt.Sprintf("%s%s%s", BLUE, "#99", RESET)) +
				link("https://github.com/a/b/commit/123abc", ")"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linkify(tt.msg, tt.github, tt.hash)
			if got != tt.expected {
				t.Errorf("linkify(%q, %q, %q):\n  got:  %q\n  want: %q",
					tt.msg, tt.github, tt.hash, got, tt.expected)
			}
		})
	}
}
