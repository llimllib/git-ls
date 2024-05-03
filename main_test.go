package main

import (
	"fmt"
	"os"
	"testing"
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
		expected []string
	}{
		{
			name:     "empty status and files",
			status:   "",
			files:    []*File{},
			expected: []string{},
		},
		{
			name:   "single file with modified status",
			status: " M file.go",
			files: []*File{
				{entry: &mockDirEntry{name: "file.go"}},
			},
			expected: []string{" M"},
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
		},
		{
			name:   ".git directory status",
			status: "M  file1.go",
			files: []*File{
				{entry: &mockDirEntry{name: "file1.go"}},
				{entry: &mockDirEntry{name: ".git"}},
			},
			expected: []string{"M ", "*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileStatus([]byte(tt.status), tt.files)
			for i, f := range tt.files {
				if f.status != tt.expected[i] {
					t.Errorf("expected %s for %s", tt.expected[i], f.entry.Name())
				}
			}
		})
	}
}

func mockGitLog(file *File) []byte {
	switch file.entry.Name() {
	case "file1.go":
		return []byte("hash1\x002023-03-01\x00John Doe\x00john@example.com\x00Initial commit")
	case "file2.go":
		return []byte("hash2\x002023-03-02\x00Jane Smith\x00jane@example.com\x00Add new feature")
	case "file3.go":
		return []byte("hash3\x002023-03-03\x00Bob Johnson\x00bob@example.com\x00Fix a bug parsing '|' pipes")
	case "file4.go":
		return []byte("invalid output format")
	default:
		return nil
	}
}

func TestParseGitLog(t *testing.T) {
	testCases := []struct {
		name     string
		files    []*File
		expected [][]string
	}{
		{
			name: "Valid git log output",
			files: []*File{
				{entry: &mockDirEntry{name: "file1.go"}},
				{entry: &mockDirEntry{name: "file2.go"}},
				{entry: &mockDirEntry{name: "file3.go"}},
			},
			expected: [][]string{
				{"file1.go", "hash1", "2023-03-01", "John Doe", "john@example.com", "Initial commit"},
				{"file2.go", "hash2", "2023-03-02", "Jane Smith", "jane@example.com", "Add new feature"},
				{"file3.go", "hash3", "2023-03-03", "Bob Johnson", "bob@example.com", "Fix a bug parsing '|' pipes"},
			},
		},
		// Is it worth bothering to test log.Fatal scenarios?
		// {
		// 	name: "Invalid git log output",
		// 	files: []*File{
		// 		{entry: &mockDirEntry{name: "file4.go"}},
		// 	},
		// 	expected: [][]string{{"file4.go"}},
		// },
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parseGitLog(tc.files, mockGitLog)

			for i, file := range tc.files {
				expected := tc.expected[i]
				if file.hash != expected[1] {
					t.Errorf("Unexpected hash for file %s: got %s, want %s", file.entry.Name(), file.hash, expected[1])
				}
				if file.lastModified != expected[2] {
					t.Errorf("Unexpected lastModified for file %s: got %s, want %s", file.entry.Name(), file.lastModified, expected[2])
				}
				if file.author != expected[3] {
					t.Errorf("Unexpected author for file %s: got %s, want %s", file.entry.Name(), file.author, expected[3])
				}
				if file.authorEmail != expected[4] {
					t.Errorf("Unexpected authorEmail for file %s: got %s, want %s", file.entry.Name(), file.authorEmail, expected[4])
				}
				if file.message != expected[5] {
					t.Errorf("Unexpected message for file %s: got %s, want %s", file.entry.Name(), file.message, expected[5])
				}
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
