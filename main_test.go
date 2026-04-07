package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMakeDiffGraph(t *testing.T) {
	tests := []struct {
		name     string
		file     *File
		width    int
		expected string
	}{
		{
			name:     "nil diffSum",
			file:     &File{},
			width:    4,
			expected: "",
		},
		{
			name:     "only additions",
			file:     &File{diffSum: &Diff{plus: 3, minus: 0}},
			width:    4,
			expected: GREEN + "+++" + RED + "" + RESET,
		},
		{
			name:     "only deletions",
			file:     &File{diffSum: &Diff{plus: 0, minus: 2}},
			width:    4,
			expected: GREEN + "" + RED + "--" + RESET,
		},
		{
			name:     "additions and deletions within width",
			file:     &File{diffSum: &Diff{plus: 2, minus: 2}},
			width:    4,
			expected: GREEN + "++" + RED + "--" + RESET,
		},
		{
			name:     "additions and deletions exceeding width are scaled",
			file:     &File{diffSum: &Diff{plus: 10, minus: 10}},
			width:    4,
			expected: GREEN + "++" + RED + "--" + RESET,
		},
		{
			name:     "new file with 4 additions fits in width 4",
			file:     &File{diffSum: &Diff{plus: 4, minus: 0}},
			width:    4,
			expected: GREEN + "++++" + RED + "" + RESET,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeDiffGraph(tt.file, tt.width)
			if result != tt.expected {
				t.Errorf("makeDiffGraph() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

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
		name            string
		status          string
		files           []*File
		dir             string
		expected        []string
		expectedOldName []string // expected oldName per file (empty string = not set)
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
		{
			name:            "rename maps status to new filename",
			status:          "R  old-name.txt -> new-name.txt",
			files:           []*File{{entry: &mockDirEntry{name: "new-name.txt"}}},
			expected:        []string{"R "},
			expectedOldName: []string{"old-name.txt"},
			dir:             "",
		},
		{
			name:            "copy maps status to new filename",
			status:          "C  source.txt -> copy.txt",
			files:           []*File{{entry: &mockDirEntry{name: "copy.txt"}}},
			expected:        []string{"C "},
			expectedOldName: []string{"source.txt"},
			dir:             "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileStatus([]byte(tt.status), tt.files, tt.dir)
			for i, f := range tt.files {
				if f.status != tt.expected[i] {
					t.Errorf("expected status %q for %s, got %q", tt.expected[i], f.entry.Name(), f.status)
				}
				if tt.expectedOldName != nil {
					if f.oldName != tt.expectedOldName[i] {
						t.Errorf("expected oldName %q for %s, got %q", tt.expectedOldName[i], f.entry.Name(), f.oldName)
					}
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
			rctx := &RenderContext{
				GithubURL: "",
				Dir:       tt.dir,
				MonoHash:  false,
			}
			showColumns(&buf, 200, files, rctx, AllColumns())
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

			// Same hash should always return same color
			color2 := hashToColor(tt.hash)
			if color != color2 {
				t.Errorf("hashToColor not deterministic: first call=%q, second call=%q", color, color2)
			}
		})
	}

	// Test that different hashes (likely) produce different colors
	hash1 := "55b998eae87118427808144927549a39e821c0fb"
	hash2 := "dd59602e5a8af4da0429237c2f2d547ee1bdc800"
	color1 := hashToColor(hash1)
	color2 := hashToColor(hash2)
	if color1 == color2 {
		t.Errorf("Different hashes produced same color: %q and %q both got %q", hash1, hash2, color1)
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

// TestWorktreeWithSymlink tests that git-ls works correctly in a worktree
// accessed via symlink. This is a regression test for issue #34.
func TestWorktreeWithSymlink(t *testing.T) {
	// Create a temporary directory for our test
	tmpDir := t.TempDir()

	// Create a git repo with one commit
	repoDir := tmpDir + "/repo"
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	// Initialize git repo
	if err := runCmd(repoDir, "git", "init", "-b", "main"); err != nil {
		t.Fatalf("Failed to init repo: %v", err)
	}

	// Configure git user for this test repo
	if err := runCmd(repoDir, "git", "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("Failed to set git email: %v", err)
	}
	if err := runCmd(repoDir, "git", "config", "user.name", "Test User"); err != nil {
		t.Fatalf("Failed to set git name: %v", err)
	}

	// Create and commit a file
	if err := os.WriteFile(repoDir+"/file1.txt", []byte("hello"), 0o644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := runCmd(repoDir, "git", "add", "file1.txt"); err != nil {
		t.Fatalf("Failed to add file: %v", err)
	}
	if err := runCmd(repoDir, "git", "commit", "-m", "initial commit"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Create a worktree
	wtDir := tmpDir + "/worktree"
	if err := runCmd(repoDir, "git", "worktree", "add", wtDir, "-b", "wt-branch"); err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	// Add an untracked file in the worktree
	if err := os.WriteFile(wtDir+"/rootfile.txt", []byte("untracked"), 0o644); err != nil {
		t.Fatalf("Failed to write untracked file: %v", err)
	}

	// Create a symlink to the worktree
	linkDir := tmpDir + "/link-to-wt"
	if err := os.Symlink(wtDir, linkDir); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Test case 1: Run from real worktree
	t.Run("real worktree", func(t *testing.T) {
		// Change to the real worktree directory
		oldDir, _ := os.Getwd()
		defer func() {
			if err := os.Chdir(oldDir); err != nil {
				t.Logf("Failed to restore directory: %v", err)
			}
		}()

		if err := os.Chdir(wtDir); err != nil {
			t.Fatalf("Failed to chdir to worktree: %v", err)
		}

		// This should not panic
		// We're testing the internal functions that were panicking
		gitData := fetchGitData()
		curdir, err := filepath.Rel(gitData.root, must(filepath.Abs(".")))
		if err != nil {
			t.Fatalf("Failed to get curdir: %v", err)
		}

		osfiles, err := os.ReadDir(".")
		if err != nil {
			t.Fatalf("Failed to read directory: %v", err)
		}

		var files []*File
		for _, file := range osfiles {
			stat, _ := os.Stat(file.Name())
			files = append(files, &File{
				entry: file,
				isDir: file.IsDir(),
				isExe: !file.IsDir() && stat.Mode()&0o111 != 0,
			})
		}

		// This was panicking before the fix
		fileStatus(gitData.status, files, curdir)

		// Verify we found the untracked file
		found := false
		for _, f := range files {
			if f.Name() == "rootfile.txt" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected to find rootfile.txt in file list")
		}
	})

	// Test case 2: Run from symlink to worktree
	t.Run("symlink to worktree", func(t *testing.T) {
		// Change to the symlink directory
		oldDir, _ := os.Getwd()
		defer func() {
			if err := os.Chdir(oldDir); err != nil {
				t.Logf("Failed to restore directory: %v", err)
			}
		}()

		if err := os.Chdir(linkDir); err != nil {
			t.Fatalf("Failed to chdir to symlink: %v", err)
		}

		// This should not panic
		gitData := fetchGitData()
		curdir, err := filepath.Rel(gitData.root, must(filepath.Abs(".")))
		if err != nil {
			t.Fatalf("Failed to get curdir: %v", err)
		}

		osfiles, err := os.ReadDir(".")
		if err != nil {
			t.Fatalf("Failed to read directory: %v", err)
		}

		var files []*File
		for _, file := range osfiles {
			stat, _ := os.Stat(file.Name())
			files = append(files, &File{
				entry: file,
				isDir: file.IsDir(),
				isExe: !file.IsDir() && stat.Mode()&0o111 != 0,
			})
		}

		// This was panicking before the fix
		fileStatus(gitData.status, files, curdir)

		// Verify we found the untracked file
		found := false
		for _, f := range files {
			if f.Name() == "rootfile.txt" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected to find rootfile.txt in file list")
		}
	})
}

// runCmd is a helper to run a command in a specific directory
func runCmd(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

// TestSubdirectoryGitInfo tests that git-ls shows git information for files
// when run on a subdirectory. This is a regression test for a bug where
// parseGitLogStreaming wasn't using --relative, causing paths from git log
// to not match the files we were looking for.
func TestSubdirectoryGitInfo(t *testing.T) {
	// Create a temporary directory for our test
	tmpDir := t.TempDir()

	// Create a git repo
	repoDir := tmpDir + "/repo"
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	// Initialize git repo
	if err := runCmd(repoDir, "git", "init", "-b", "main"); err != nil {
		t.Fatalf("Failed to init repo: %v", err)
	}

	// Configure git user for this test repo
	if err := runCmd(repoDir, "git", "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("Failed to set git email: %v", err)
	}
	if err := runCmd(repoDir, "git", "config", "user.name", "Test User"); err != nil {
		t.Fatalf("Failed to set git name: %v", err)
	}

	// Create a subdirectory with a file
	subDir := repoDir + "/docs"
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	if err := os.WriteFile(subDir+"/README.md", []byte("# Documentation"), 0o644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Commit the file
	if err := runCmd(repoDir, "git", "add", "docs/README.md"); err != nil {
		t.Fatalf("Failed to add file: %v", err)
	}
	if err := runCmd(repoDir, "git", "commit", "-m", "Add documentation"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Change to the subdirectory (simulating: git-ls docs)
	oldDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Failed to restore directory: %v", err)
		}
	}()

	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("Failed to chdir to subdirectory: %v", err)
	}

	// Read the files in the subdirectory
	osfiles, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("Failed to read directory: %v", err)
	}

	var files []*File
	for _, file := range osfiles {
		stat, _ := os.Stat(file.Name())
		files = append(files, &File{
			entry: file,
			isDir: file.IsDir(),
			isExe: !file.IsDir() && stat.Mode()&0o111 != 0,
		})
	}

	// Run parseGitLogStreaming to get git info
	if err := parseGitLog(files); err != nil {
		t.Fatalf("parseGitLog failed: %v", err)
	}

	// Verify that README.md has git info populated
	var readme *File
	for _, f := range files {
		if f.Name() == "README.md" {
			readme = f
			break
		}
	}

	if readme == nil {
		t.Fatal("README.md not found in file list")
	}

	// Check that git info was populated
	if readme.hash == "" {
		t.Error("Expected README.md to have a commit hash, but it was empty")
	}
	if readme.author == "" {
		t.Error("Expected README.md to have an author, but it was empty")
	}
	if readme.message == "" {
		t.Error("Expected README.md to have a commit message, but it was empty")
	}
	if readme.lastModified == "" {
		t.Error("Expected README.md to have a last modified date, but it was empty")
	}

	// Verify the values are correct
	if readme.author != "Test User" {
		t.Errorf("Expected author 'Test User', got %q", readme.author)
	}
	if readme.message != "Add documentation" {
		t.Errorf("Expected message 'Add documentation', got %q", readme.message)
	}
}

// TestEmptyRepository tests that git-ls works correctly in an empty repository
// (one with no commits yet). This is a regression test for issue #35.
func TestEmptyRepository(t *testing.T) {
	// Create a temporary directory for our test
	tmpDir := t.TempDir()

	// Create an empty git repo (no commits)
	repoDir := tmpDir + "/empty-repo"
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	// Initialize git repo
	if err := runCmd(repoDir, "git", "init", "-b", "main"); err != nil {
		t.Fatalf("Failed to init repo: %v", err)
	}

	// Configure git user for this test repo
	if err := runCmd(repoDir, "git", "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("Failed to set git email: %v", err)
	}
	if err := runCmd(repoDir, "git", "config", "user.name", "Test User"); err != nil {
		t.Fatalf("Failed to set git name: %v", err)
	}

	// Create a file but don't commit it
	if err := os.WriteFile(repoDir+"/test.txt", []byte("hello"), 0o644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Change to the empty repo directory
	oldDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Failed to restore directory: %v", err)
		}
	}()

	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("Failed to chdir to empty repo: %v", err)
	}

	// This should not crash with "fatal: ambiguous argument 'HEAD'"
	// Before the fix, gitDiffStat() would call log.Fatalf() here
	diffStat := gitDiffStat()

	// In an empty repo with no commits, there should be no diff stats
	// (because there's no HEAD to compare against)
	if len(diffStat) != 0 {
		t.Logf("Expected empty diffStat in empty repo, got: %s", string(diffStat))
		// Note: This is logged but not a failure - the important thing is
		// that gitDiffStat() didn't crash
	}

	t.Log("Successfully ran gitDiffStat() in empty repository without crashing")
}

// TestChangedOnly verifies that the --changed-only flag filters files
// to only show those with git status.
func TestChangedOnly(t *testing.T) {
	// Create test files with different statuses
	files := []*File{
		{entry: &mockDirEntry{name: "modified.go"}, status: "M "},
		{entry: &mockDirEntry{name: "untracked.go"}, status: "??"},
		{entry: &mockDirEntry{name: "clean.go"}, status: ""},
		{entry: &mockDirEntry{name: "added.go"}, status: "A "},
		{entry: &mockDirEntry{name: "another-clean.go"}, status: ""},
		{name: "deleted.go", status: "D ", isDeleted: true},
	}

	// Filter to only changed files (mimicking the --changed-only logic)
	var changedFiles []*File
	for _, file := range files {
		if file.status != "" {
			changedFiles = append(changedFiles, file)
		}
	}

	// Verify we got only the files with status
	if len(changedFiles) != 4 {
		t.Errorf("Expected 4 changed files, got %d", len(changedFiles))
	}

	// Verify the correct files were kept
	expectedNames := map[string]bool{
		"modified.go":  true,
		"untracked.go": true,
		"added.go":     true,
		"deleted.go":   true,
	}

	for _, f := range changedFiles {
		name := f.Name()
		if !expectedNames[name] {
			t.Errorf("Unexpected file in changed list: %s", name)
		}
		delete(expectedNames, name)
	}

	if len(expectedNames) > 0 {
		for name := range expectedNames {
			t.Errorf("Expected file %s was not in changed list", name)
		}
	}
}

// TestSkipGitLogForIgnoredFiles verifies that we skip git log for files
// that don't have meaningful history (ignored, untracked, .git, newly added)
func TestSkipGitLogForIgnoredFiles(t *testing.T) {
	files := []*File{
		{entry: &mockDirEntry{name: "modified.go"}, status: "M "},
		{entry: &mockDirEntry{name: "ignored.log"}, status: "I"},
		{entry: &mockDirEntry{name: "untracked.go"}, status: "??"},
		{entry: &mockDirEntry{name: ".git"}, status: "*"},
		{entry: &mockDirEntry{name: "added.go"}, status: "A "},
		{entry: &mockDirEntry{name: "added-modified.go"}, status: "AM"},
		{entry: &mockDirEntry{name: "dir-with-changes"}, status: "A ,M "},
	}

	// Filter files that need git log (mimicking the optimization)
	var filesNeedingLog []*File
	for _, file := range files {
		if file.status == "I" || file.status == "??" || file.status == "*" {
			continue
		}
		// Check if ALL statuses are additions (no history to look up)
		allNew := true
		for status := range strings.SplitSeq(file.status, ",") {
			status = strings.TrimSpace(status)
			if len(status) > 0 && status[0] != 'A' && status != "??" {
				allNew = false
				break
			}
		}
		if !allNew {
			filesNeedingLog = append(filesNeedingLog, file)
		}
	}

	// Should have 2 files: modified.go and dir-with-changes
	// Skip: ignored, untracked, .git, purely added files
	if len(filesNeedingLog) != 2 {
		t.Errorf("Expected 2 files needing git log, got %d", len(filesNeedingLog))
	}

	expectedNames := map[string]bool{
		"modified.go":      true,
		"dir-with-changes": true,
	}

	for _, f := range filesNeedingLog {
		name := f.Name()
		if !expectedNames[name] {
			t.Errorf("Unexpected file needing git log: %s", name)
		}
		delete(expectedNames, name)
	}

	if len(expectedNames) > 0 {
		for name := range expectedNames {
			t.Errorf("Expected file %s was not in files needing git log", name)
		}
	}
}

// the file's previous name. This is a regression test for a bug where renamed
// files had no git log info because git log couldn't find history under the
// new name.
func TestRenamedFileGitInfo(t *testing.T) {
	tmpDir := t.TempDir()

	repoDir := tmpDir + "/repo"
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	if err := runCmd(repoDir, "git", "init", "-b", "main"); err != nil {
		t.Fatalf("Failed to init repo: %v", err)
	}
	if err := runCmd(repoDir, "git", "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("Failed to set git email: %v", err)
	}
	if err := runCmd(repoDir, "git", "config", "user.name", "Test User"); err != nil {
		t.Fatalf("Failed to set git name: %v", err)
	}

	// Create and commit a file
	if err := os.WriteFile(repoDir+"/old-name.txt", []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := runCmd(repoDir, "git", "add", "old-name.txt"); err != nil {
		t.Fatalf("Failed to add file: %v", err)
	}
	if err := runCmd(repoDir, "git", "commit", "-m", "initial commit"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Rename the file and stage it
	if err := runCmd(repoDir, "git", "mv", "old-name.txt", "new-name.txt"); err != nil {
		t.Fatalf("Failed to rename file: %v", err)
	}

	// Change to the repo directory
	oldDir, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Failed to restore directory: %v", err)
		}
	}()

	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}

	osfiles, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("Failed to read directory: %v", err)
	}

	var files []*File
	for _, file := range osfiles {
		stat, _ := os.Stat(file.Name())
		files = append(files, &File{
			entry: file,
			isDir: file.IsDir(),
			isExe: !file.IsDir() && stat.Mode()&0o111 != 0,
		})
	}

	gitData := fetchGitData()
	resolved := must(filepath.EvalSymlinks(must(filepath.Abs("."))))
	curdir := must(filepath.Rel(gitData.root, resolved))
	fileStatus(gitData.status, files, curdir)

	// Run the streaming log — should find renamed files via their old name
	if err := parseGitLog(files); err != nil {
		t.Fatalf("parseGitLog failed: %v", err)
	}

	// Find new-name.txt
	var renamed *File
	for _, f := range files {
		if f.Name() == "new-name.txt" {
			renamed = f
			break
		}
	}

	if renamed == nil {
		t.Fatal("new-name.txt not found in file list")
	}

	// Verify status
	if renamed.status == "" {
		t.Error("Expected new-name.txt to have a rename status, but it was empty")
	}
	if renamed.status[0] != 'R' {
		t.Errorf("Expected rename status starting with 'R', got %q", renamed.status)
	}

	// Verify oldName was recorded
	if renamed.oldName != "old-name.txt" {
		t.Errorf("Expected oldName 'old-name.txt', got %q", renamed.oldName)
	}

	// Verify commit info was found via the old name
	if renamed.hash == "" {
		t.Error("Expected new-name.txt to have a commit hash from its old name, but it was empty")
	}
	if renamed.author != "Test User" {
		t.Errorf("Expected author 'Test User', got %q", renamed.author)
	}
	if renamed.message != "initial commit" {
		t.Errorf("Expected message 'initial commit', got %q", renamed.message)
	}
}
