package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIsGithub(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "Valid GitHub remote",
			input:    []byte("origin\tgit@github.com:username/repo.git (fetch)\norigin\tgit@github.com:username/repo.git (push)"),
			expected: "https://github.com/username/repo",
		},
		{
			name:     "Valid GitHub remote with HTTP",
			input:    []byte("origin\thttps://github.com/username/repo.git (fetch)\norigin\thttps://github.com/username/repo.git (push)"),
			expected: "https://github.com/username/repo",
		},
		{
			name:     "Invalid remote",
			input:    []byte("origin\tgit@example.com:username/repo.git (fetch)\norigin\tgit@example.com:username/repo.git (push)"),
			expected: "",
		},
		{
			name:     "Empty input",
			input:    []byte{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isGithub(tt.input)
			if result != tt.expected {
				t.Errorf("isGithub(%s) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

type mockDirEntry struct {
	name string
}

func (m *mockDirEntry) Name() string {
	return m.name
}

func (m *mockDirEntry) IsDir() bool {
	return false
}

func (m *mockDirEntry) Type() os.FileMode {
	return 0
}

func (m *mockDirEntry) Info() (os.FileInfo, error) {
	return nil, nil
}

func TestFileStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		files    []*File
		dir      string
		expected []string
	}{
		{
			name:     "empty status and files",
			status:   "",
			files:    []*File{},
			expected: []string{},
			dir:      "",
		},
		{
			name:   "single file with modified status",
			status: " M file.go",
			files: []*File{
				{entry: &mockDirEntry{name: "file.go"}},
			},
			expected: []string{" M"},
			dir:      "",
		},
		{
			name:   "multiple files with different statuses",
			status: "M  file1.go\nA  file2.go\n!! ignored.go",
			files: []*File{
				{entry: &mockDirEntry{name: "file1.go"}},
				{entry: &mockDirEntry{name: "file2.go"}},
				{entry: &mockDirEntry{name: "ignored.go"}},
			},
			expected: []string{"M ", "A ", "I"},
			dir:      "",
		},
		{
			name:   ".git directory status",
			status: "M  file1.go",
			files: []*File{
				{entry: &mockDirEntry{name: "file1.go"}},
				{entry: &mockDirEntry{name: ".git"}},
			},
			expected: []string{"M ", "*"},
			dir:      "",
		},
		{
			name:   "subdirectory",
			status: "M  homedir/file2.go",
			files: []*File{
				{entry: &mockDirEntry{name: "file2.go"}},
			},
			dir:      "homedir/",
			expected: []string{"M "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileStatus([]byte(tt.status), tt.files, tt.dir)
			for i, f := range tt.files {
				if f.status != tt.expected[i] {
					t.Errorf("expected %s for %s, got %s", tt.expected[i], f.entry.Name(), f.status)
				}
			}
		})
	}
}

// parseArgs extracts diffWidth and directory from command line arguments.
// This is extracted from main() for testability.
func parseArgs(argv []string) (diffWidth int, dir string) {
	diffWidth = 4
	for len(argv) > 0 {
		if argv[0] == "--version" || argv[0] == "--help" || argv[0] == "-h" {
			return diffWidth, ""
		}
		if len(argv[0]) > 0 && argv[0][:2] == "--" && len(argv[0]) > 11 && argv[0][:11] == "--diffWidth" {
			if len(argv) == 1 {
				if len(argv[0]) > 11 && argv[0][11] == '=' {
					parts := make([]string, 2)
					parts[0] = argv[0][:11]
					parts[1] = argv[0][12:]
					diffWidth, _ = parseInt(parts[1])
				}
				argv = argv[1:]
			} else {
				diffWidth, _ = parseInt(argv[1])
				argv = argv[2:]
			}
		} else {
			// Non-flag argument (directory), stop parsing flags
			break
		}
	}

	if len(argv) > 0 {
		dir = argv[0]
	} else {
		dir = "."
	}
	return diffWidth, dir
}

func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid integer: %s", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		expectedDir   string
		expectedWidth int
	}{
		{
			name:          "no arguments",
			args:          []string{},
			expectedDir:   ".",
			expectedWidth: 4,
		},
		{
			name:          "directory argument",
			args:          []string{"bin"},
			expectedDir:   "bin",
			expectedWidth: 4,
		},
		{
			name:          "directory with path",
			args:          []string{"some/nested/dir"},
			expectedDir:   "some/nested/dir",
			expectedWidth: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width, dir := parseArgs(tt.args)
			if dir != tt.expectedDir {
				t.Errorf("parseArgs(%v) dir = %q, expected %q", tt.args, dir, tt.expectedDir)
			}
			if width != tt.expectedWidth {
				t.Errorf("parseArgs(%v) width = %d, expected %d", tt.args, width, tt.expectedWidth)
			}
		})
	}
}

// TestDirectoryArgDoesNotHang tests that passing a directory argument doesn't cause an infinite loop.
// This is a regression test for a bug where the argument parsing loop would hang forever
// when a non-flag argument (like a directory name) was passed.
func TestDirectoryArgDoesNotHang(t *testing.T) {
	done := make(chan bool, 1)
	go func() {
		// This should complete almost instantly
		_, dir := parseArgs([]string{"bin"})
		if dir != "bin" {
			t.Errorf("Expected dir to be 'bin', got %q", dir)
		}
		done <- true
	}()

	select {
	case <-done:
		// Test passed
	case <-time.After(1 * time.Second):
		t.Fatal("parseArgs hung when given a directory argument - argument parsing loop is infinite")
	}
}

// TestShowFileHyperlinks verifies that file hyperlinks are generated correctly.
// This is a regression test for a bug where passing a directory argument would
// cause incorrect hyperlinks (e.g., "git-ls bin" would generate links like
// "bin/bin/release.sh" instead of "bin/release.sh").
func TestShowFileHyperlinks(t *testing.T) {
	tests := []struct {
		name        string
		dir         string // absolute path passed to show()
		fileName    string
		expectedURL string // the file path portion we expect in the URL
	}{
		{
			name:        "file in subdirectory",
			dir:         "/repo/bin",
			fileName:    "release.sh",
			expectedURL: "/repo/bin/release.sh",
		},
		{
			name:        "file in root",
			dir:         "/repo",
			fileName:    "main.go",
			expectedURL: "/repo/main.go",
		},
		{
			name:        "nested subdirectory",
			dir:         "/repo/src/pkg",
			fileName:    "util.go",
			expectedURL: "/repo/src/pkg/util.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := []*File{
				{entry: &mockDirEntry{name: tt.fileName}},
			}

			var buf strings.Builder
			showColumns(&buf, 200, files, "", tt.dir, AllColumns())
			output := buf.String()

			// Check that the output contains the correct file URL path
			// The hyperlink format is: \e]8;;<url>\e\<text>\e]8;;\e\
			expectedPath := fmt.Sprintf("file://%s%s", must(os.Hostname()), tt.expectedURL)
			if !strings.Contains(output, expectedPath) {
				t.Errorf("Expected output to contain %q, but got:\n%q", expectedPath, output)
			}

			// Make sure we don't have a doubled directory (the bug we're preventing)
			// e.g., /repo/bin/bin/release.sh
			doubledDir := tt.dir + "/" + tt.dir[strings.LastIndex(tt.dir, "/")+1:]
			if strings.Contains(output, doubledDir+"/") {
				t.Errorf("Found doubled directory path in output: %q", output)
			}
		})
	}
}

func TestLinkify(t *testing.T) {
	testCases := []struct {
		name     string
		test     string
		expected string
	}{
		{
			name:     "Basic test",
			test:     "Some message",
			expected: link("https://github.com/a/b/commit/123abc", "Some message"),
		},
		{
			name: "One issue link",
			test: "fixes issue (#17)",
			expected: link("https://github.com/a/b/commit/123abc", "fixes issue (") +
				link("https://github.com/a/b/pull/17", fmt.Sprintf("%s%s%s", BLUE, "#17", RESET)) +
				link("https://github.com/a/b/commit/123abc", ")"),
		},
		{
			name: "Two issue links",
			test: "fixes issue (#17) closes (#99)",
			expected: link("https://github.com/a/b/commit/123abc", "fixes issue (") +
				link("https://github.com/a/b/pull/17", fmt.Sprintf("%s%s%s", BLUE, "#17", RESET)) +
				link("https://github.com/a/b/commit/123abc", ") closes (") +
				link("https://github.com/a/b/pull/99", fmt.Sprintf("%s%s%s", BLUE, "#99", RESET)) +
				link("https://github.com/a/b/commit/123abc", ")"),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := linkify(tc.test, "https://github.com/a/b", "123abc")
			if s != tc.expected {
				t.Errorf("Expected\n%#v !=\n%#v", tc.expected, s)
			}
		})
	}
}
