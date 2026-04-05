package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/mattn/go-runewidth"
)

const VERSION = "5.4.0"

// HistoryLimit is the maximum number of commits to review; we'll exit if we
// find all files before it; if a file was more than this many commits ago we
// won't print its info
const HistoryLimit = "50000"

type Diff struct {
	plus  int
	minus int
}

type File struct {
	entry        os.DirEntry
	name         string // used for deleted files that don't have a DirEntry
	oldName      string // pre-rename path for R/C status (relative to current directory)
	status       string
	diffSum      *Diff
	diffStat     string
	author       string
	authorEmail  string
	hash         string // full hash
	shortHash    string // Git's abbreviated hash (length varies by repo size)
	lastModified string
	message      string
	isDir        bool
	isExe        bool
	isDeleted    bool
}

// RenderContext holds settings that affect how output is rendered
type RenderContext struct {
	GithubURL string
	Dir       string
	MonoHash  bool
	NerdFont  bool
}

// Name returns the file name, either from the DirEntry or the name field
func (f *File) Name() string {
	if f.entry != nil {
		return f.entry.Name()
	}
	return f.name
}

const (
	BLUE      = "\x1b[34m"
	CYAN      = "\x1b[36m"
	GREEN     = "\x1b[32m"
	RED       = "\x1b[31m"
	RESET     = "\x1b[0m"
	YELLOW    = "\x1b[33m"
	STRIKEOUT = "\x1b[9m"
)

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

func must[T any](a T, e error) T {
	if e != nil {
		panic(e)
	}
	return a
}

// gitResults holds the output of all git commands that can be run in parallel
type gitResults struct {
	root          string
	status        []byte
	diffStat      []byte
	currentBranch string
	remotes       []byte
	isEmpty       bool // true if repo has no commits yet
}

// isEmptyRepo checks if the repository has no commits yet by verifying if HEAD
// exists. If it doesn't, this could mean:
//
//  1. We're in an empty repo (no commits yet) - the case we care about
//
//  2. We're not in a git repo at all - this will be caught by gitRoot() or
//     gitStatus()
//
// We check exit code 128 (git's fatal error code) because it's the best signal
// I found for "empty repository"
func isEmptyRepo() bool {
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "rev-parse", "--verify", "HEAD")
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode() == 128
		}
	}
	return false
}

// fetchGitData runs all independent git commands in parallel and returns the results
func fetchGitData() *gitResults {
	results := &gitResults{}

	var wg sync.WaitGroup

	wg.Go(func() {
		results.root = gitRoot()
	})

	wg.Go(func() {
		results.status = gitStatus()
	})

	wg.Go(func() {
		results.currentBranch = gitCurrentBranchSymbolic()
	})

	wg.Go(func() {
		results.remotes = gitRemotes()
	})

	// No diff stats in empty repo
	if isEmptyRepo() {
		results.isEmpty = true
		results.diffStat = []byte{}
	} else {
		wg.Go(func() {
			results.diffStat = gitDiffStat()
		})
	}

	wg.Wait()
	return results
}

func usage() {
	fmt.Printf(`GIT-LS(1)

NAME
    git-ls - show the current directory annotated with links and git info

SYNOPSIS
    git ls [<dir>]

DESCRIPTION
    Displays the files in the current directory, their current git status, a short diffstat, their last modified date, the author and a portion of the last commit message for that file.

    All files are hyperlinked with OSC8 hyperlinks, so you should be able to open them by clicking on them in a properly-configured terminal. The author names are hyperlinked to github if the repository has a github remote, as are commit messages.

OPTIONS
    --version
        Print the version number and exit

    --help
        Print this message and exit

    --diffWidth=n
        Print the diffStat graph with the given width. Default is 4

    --format=col1,col2,...
        Specify which columns to display and in what order. Valid columns:
        status, diff, filename, shorthash, hash, date, author, email,
        numstat, commitmessage
        Default: status,diff,filename,shorthash,date,author,commitmessage

    --mono-hash
        Use a single color (cyan) for all commit hashes instead of coloring
        each hash uniquely based on its value

    --nerdfont
        Replace git status letters with Nerd Font icons (requires a Nerd
        Font-patched terminal font)

%s
`, link("https://github.com/llimllib/git-ls", "https://github.com/llimllib/git-ls"))
}

func main() {
	argv := os.Args[1:]
	diffWidth := 4
	monoHash := false
	nerdFont := false
	var formatColumns []Column
	for len(argv) > 0 {
		if argv[0] == "--version" {
			fmt.Printf("%s\n", VERSION)
			os.Exit(0)
		}
		if argv[0] == "--help" || argv[0] == "-h" {
			usage()
			os.Exit(0)
		}
		if argv[0] == "--mono-hash" {
			monoHash = true
			argv = argv[1:]
		} else if argv[0] == "--nerdfont" {
			nerdFont = true
			argv = argv[1:]
		} else if strings.HasPrefix(argv[0], "--diffWidth") {
			if len(argv) == 1 {
				if strings.Contains(argv[0], "=") {
					parts := strings.SplitN(argv[0], "=", 2)
					diffWidth = must(strconv.Atoi(parts[1]))
				} else {
					log.Fatalf("--diffWidth requires an argument")
				}
				argv = argv[1:]
			} else {
				diffWidth = must(strconv.Atoi(argv[1]))
				argv = argv[2:]
			}
		} else if strings.HasPrefix(argv[0], "--format") {
			if len(argv) == 1 {
				if strings.Contains(argv[0], "=") {
					parts := strings.SplitN(argv[0], "=", 2)
					formatColumns = parseFormat(parts[1])
				} else {
					log.Fatalf("--format requires an argument")
				}
				argv = argv[1:]
			} else {
				formatColumns = parseFormat(argv[1])
				argv = argv[2:]
			}
		} else {
			// Non-flag argument (directory), stop parsing flags
			break
		}
	}

	var dir string
	if len(argv) > 0 {
		dir = argv[0]

		if err := os.Chdir(dir); err != nil {
			log.Fatalf("Failed to change directory to %s: %v", dir, err)
		}
	} else {
		dir = "."
	}

	osfiles, err := os.ReadDir(".")
	if err != nil {
		log.Fatalf("Failed to read directory %s: %v", dir, err)
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

	// Fetch all git data in parallel
	gitData := fetchGitData()

	// Resolve symlinks to match git's perspective. Git internally resolves
	// symlinks when working with worktrees, so we need to do the same to
	// ensure filepath.Rel() works correctly in fileStatus().
	// https://github.com/llimllib/git-ls/issues/34
	resolved := must(filepath.EvalSymlinks(must(filepath.Abs("."))))
	curdir := must(filepath.Rel(gitData.root, resolved))
	fileStatus(gitData.status, files, curdir)
	if err := parseGitLogStreaming(files); err != nil {
		log.Printf("Warning: git log streaming failed: %v", err)
		// Fallback to parallel approach if streaming fails
		parseGitLogParallel(files)
	}
	parseDiffStat(gitData.diffStat, files)

	// generate a diffStat graph for every file
	for _, file := range files {
		file.diffStat = makeDiffGraph(file, diffWidth)
	}

	// Parse deleted files from git status and merge into file list
	deletedFiles := parseDeletedFiles(gitData.status, curdir)
	parseGitLogParallel(deletedFiles)
	files = append(files, deletedFiles...)
	slices.SortFunc(files, func(a, b *File) int {
		return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
	})

	// Use default columns if not specified
	if len(formatColumns) == 0 {
		formatColumns = AllColumns()
	}

	maxWidth := columns(os.Stdout.Fd())
	if maxWidth == 0 {
		maxWidth = 80 // default when not a TTY
	}
	fmt.Printf("On branch %s%s%s\n\n", RED, gitData.currentBranch, RESET)
	rctx := &RenderContext{
		GithubURL: isGithub(gitData.remotes),
		Dir:       must(filepath.Abs(".")),
		MonoHash:  monoHash,
		NerdFont:  nerdFont,
	}
	showColumns(os.Stdout, maxWidth, files, rctx, formatColumns)
}

func parseFormat(formatStr string) []Column {
	validCols := ValidColumns()
	colNames := strings.Split(formatStr, ",")
	var columns []Column

	for _, name := range colNames {
		name = strings.TrimSpace(name)
		if col, ok := validCols[name]; ok {
			columns = append(columns, col)
		} else {
			fmt.Fprintf(os.Stderr, "Error: Invalid column name '%s'\n", name)
			fmt.Fprintf(os.Stderr, "Valid columns: status, diff, filename, shorthash, hash, date, author, email, numstat, commitmessage\n")
			os.Exit(1)
		}
	}

	return columns
}

func link(url string, name string) string {
	// hyperlink format: \e]8;;<url>\e\<link text>\e]8;;\e\
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, name)
}

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

// width returns the printable width of a string in a terminal, by ignoring
// ansi sequences. This version assumes all characters have a width of 1, which
// is not true in general but is true in this program. modified from:
// https://github.com/muesli/ansi/blob/276c6243b/buffer.go#L21
func width(s string) int {
	// Use runewidth to properly handle Unicode characters including diacritics
	return runewidth.StringWidth(stripANSI(s))
}

// stripANSI removes ANSI escape codes from a string for width calculation
func stripANSI(s string) string {
	var result strings.Builder
	var i int
	for i < len(s) {
		if i < len(s)-1 && s[i] == '\x1b' {
			// Check for OSC sequences (hyperlinks): \x1b]8;;...\x1b\\
			if s[i+1] == ']' {
				// Find the ST (String Terminator): \x1b\\
				end := strings.Index(s[i:], "\x1b\\")
				if end != -1 {
					i += end + 2 // Skip past the entire OSC sequence
					continue
				}
			}
			// Check for CSI sequences: \x1b[...letter
			if s[i+1] == '[' {
				i += 2
				for i < len(s) {
					// @, A-Z, a-z terminate the escape
					if (s[i] >= 0x40 && s[i] <= 0x5a) || (s[i] >= 0x61 && s[i] <= 0x7a) {
						i++
						break
					}
					i++
				}
				continue
			}
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

type windowSize struct {
	rows uint16
	cols uint16
}

// from https://github.com/epam/hubctl/blob/6f86e6663/cmd/hub/lifecycle/terminal.go#L59
func columns(fd uintptr) int {
	var sz windowSize
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL,
		fd, uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&sz)))
	return int(sz.cols)
}

// Pulled straight from git:
// https://github.com/git/git/blob/d4cc1ec3/diff.c#L2862-L2874
func scaleLinear(n int, width int, maxChange int) int {
	if n == 0 {
		return 0
	}
	/*
	 * make sure that at least one '-' or '+' is printed if
	 * there is any change to this path. The easiest way is to
	 * scale linearly as if the allotted width is one column shorter
	 * than it is, and then add 1 to the result.
	 */
	return 1 + (n * (width - 1) / maxChange)
}

// makeDiffGraph turns the total diff for a file/directory into a diff graph
// string.
func makeDiffGraph(file *File, width int) string {
	if file.diffSum == nil {
		return ""
	}
	plus := file.diffSum.plus
	minus := file.diffSum.minus
	if plus+minus <= width {
		return fmt.Sprintf("%s%s%s%s%s",
			GREEN,
			strings.Repeat("+", plus),
			RED,
			strings.Repeat("-", minus),
			RESET)
	}
	return fmt.Sprintf("%s%s%s%s%s",
		GREEN,
		strings.Repeat("+", scaleLinear(plus, width, plus+minus)),
		RED,
		strings.Repeat("-", scaleLinear(minus, width, plus+minus)),
		RESET)
}

func showColumns(out io.Writer, maxWidth int, files []*File, rctx *RenderContext, columns []Column) {
	// Calculate max widths for each column
	colWidths := calculateColumnWidths(files, columns, rctx)

	// Render each file
	for _, file := range files {
		lineWidth := 0

		// Render each column in order
		for i, col := range columns {
			// Add space between columns (except before first column)
			if i > 0 {
				must(fmt.Fprintf(out, " "))
				lineWidth += 1
			}

			// Check if we have space for this column
			if lineWidth >= maxWidth {
				break
			}

			// Calculate available width for this column
			availableWidth := maxWidth - lineWidth
			colWidth := min(colWidths[col], availableWidth)

			// Render the column
			renderer := getColumnRenderer(col)
			renderer(out, file, colWidth, rctx)
			lineWidth += colWidth
		}

		// Reset any remaining formatting (like strikethrough for deleted files)
		if file.isDeleted {
			must(fmt.Fprintf(out, "%s", RESET))
		}
		must(fmt.Fprintf(out, "\n"))
	}
}

func gitRemotes() []byte {
	// disable core.fsmonitor because git will run malicious executables
	// https://github.com/califio/publications/blob/main/MADBugs/vim-vs-emacs-vs-claude/Emacs.md
	// we do this on every `git` call
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "remote", "-v")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git status: %v", err)
	}
	return out
}

func isGithub(out []byte) string {
	githubRe := regexp.MustCompile(`github.com[:/]([\w-_]+)/([\w-_]+)`)
	matches := githubRe.FindStringSubmatch(string(out))
	if len(matches) == 3 {
		return fmt.Sprintf("https://github.com/%s/%s", matches[1], matches[2])
	}
	return ""
}

// gitCurrentBranchSymbolic gets the current branch name using symbolic-ref.
// This works in empty repos (no commits) where rev-parse would fail.
func gitCurrentBranchSymbolic() string {
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "symbolic-ref", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get current branch: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// gitRoot returns the root directory of the git repository
func gitRoot() string {
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git status: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// gitStatus accepts a dir and a slice of files, and adds the git status to
// each file in place
func gitStatus() []byte {
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "status", "--porcelain", "--ignored")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git status: %v", err)
	}
	return out
}

func fileStatus(status []byte, files []*File, curdir string) {
	gitStatusMap := make(map[string][]string)
	// oldNameMap tracks the old (pre-rename) path for renamed/copied files,
	// keyed by the new filename. This is needed so git log can look up
	// the file's history under its previous name.
	oldNameMap := make(map[string]string)
	for line := range strings.SplitSeq(string(status), "\n") {
		if len(line) >= 3 {
			status := line[:2]
			// TODO: reject filenames that aren't in the current directory. Can
			// we just ignore ".." entries? Right now, if you're in /subdir,
			// and there's changes in /otherdir/whatever , this will create
			// gitStatusMap entries of "..", which doesn't seem to mess stuff
			// up but isn't ideal either
			path := line[3:]
			// For renames and copies, git's porcelain output looks like:
			//   R  old-name.txt -> new-name.txt
			//   C  source.txt -> copy.txt
			// We need the new filename (after " -> ") since that's what
			// exists on disk, and we record the old name so we can look up
			// the file's commit history.
			if (status[0] == 'R' || status[0] == 'C') && strings.Contains(path, " -> ") {
				parts := strings.SplitN(path, " -> ", 2)
				oldNameMap[first(must(filepath.Rel(curdir, parts[1])))] = must(filepath.Rel(curdir, parts[0]))
				path = parts[1]
			}
			fileName := first(must(filepath.Rel(curdir, path)))
			if status == "!!" {
				status = "I"
			}
			gitStatusMap[fileName] = append(gitStatusMap[fileName], status)
		}
	}

	for _, file := range files {
		if fileStatus, ok := gitStatusMap[file.Name()]; ok {
			slices.Sort(fileStatus)
			file.status = strings.Join(slices.Compact(fileStatus), ",")
		}
		if oldName, ok := oldNameMap[file.Name()]; ok {
			file.oldName = oldName
		}
		if file.Name() == ".git" {
			file.status = "*"
		}
	}
}

// parseDeletedFiles extracts deleted files from git status output and returns
// them as File structs. Deleted files have status " D" (deleted in worktree)
// or "D " (staged deletion).
func parseDeletedFiles(status []byte, curdir string) []*File {
	var deletedFiles []*File
	for line := range strings.SplitSeq(string(status), "\n") {
		if len(line) >= 3 {
			statusCode := line[:2]
			// " D" means deleted in worktree, "D " means staged deletion
			if statusCode == " D" || statusCode == "D " {
				fileName := first(must(filepath.Rel(curdir, line[3:])))
				// Only include files in current directory (not ".." for parent dirs)
				if fileName != ".." && !strings.Contains(fileName, string(os.PathSeparator)) {
					deletedFiles = append(deletedFiles, &File{
						name:      fileName,
						status:    statusCode,
						isDeleted: true,
					})
				}
			}
		}
	}
	return deletedFiles
}

// gitLogResult holds the result of a git log call for a single file
type gitLogResult struct {
	file   *File
	output []byte
}

// parseGitLogStreaming runs a single git log command and streams the output,
// stopping as soon as all files are found. This is much faster than spawning
// N processes because:
// 1. Single process (no spawn overhead)
// 2. Early exit (stops when all files found)
// 3. Walks history once (all files benefit from git's caching)
// 4. Directory scoped (only checks current directory)
func parseGitLogStreaming(files []*File) error {
	if len(files) == 0 {
		return nil
	}

	// Build map of files we need to find
	filesNeeded := make(map[string]*File)
	for _, f := range files {
		filesNeeded[f.Name()] = f
		// For renamed/copied files, also look for the old name so the
		// streaming log can match the file under its previous path.
		if f.oldName != "" {
			filesNeeded[first(f.oldName)] = f
		}
	}

	// Start git log with streaming output
	// -- .: limit to current directory (faster)
	// --relative: make paths relative to current directory
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "log",
		"--name-only",
		"--relative",
		"--date=format:%Y-%m-%d",
		"--format=%H%x00%h%x00%ad%x00%aN%x00%aE%x00%s%x00",
		"-n", HistoryLimit,
		"HEAD", "--", ".")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	var currentCommit struct {
		hash        string
		shortHash   string
		date        string
		author      string
		authorEmail string
		message     string
	}

	// Track commits since last find for early exit
	commitsSinceLastFind := 0
	const giveUpAfter = 5000 // If no new files found in 5000 commits, stop

	for scanner.Scan() {
		commitsSinceLastFind++
		line := scanner.Text()

		if len(line) == 0 {
			continue // Skip blank lines
		}

		// Check if this is a commit metadata line (contains null bytes)
		if strings.Contains(line, "\x00") {
			parts := strings.Split(line, "\x00")
			if len(parts) >= 6 {
				currentCommit.hash = parts[0]
				currentCommit.shortHash = parts[1]
				currentCommit.date = parts[2]
				currentCommit.author = parts[3]
				currentCommit.authorEmail = parts[4]
				currentCommit.message = parts[5]
			}
		} else if currentCommit.hash != "" {
			// This is a filename line
			filename := strings.TrimSpace(line)

			// Extract the first component (for directories)
			// e.g., "builtin/add.c" -> "builtin", "main.go" -> "main.go"
			firstPart := first(filename)

			// Check if this is a file/dir we're looking for
			if file, needed := filesNeeded[firstPart]; needed {
				// Found it! Populate the file info
				file.hash = currentCommit.hash
				file.shortHash = currentCommit.shortHash
				file.lastModified = currentCommit.date
				file.author = currentCommit.author
				file.authorEmail = currentCommit.authorEmail
				file.message = currentCommit.message

				// Remove from needed set. For renamed/copied files we
				// may have two keys (old name + new name) pointing to
				// the same File, so delete both.
				delete(filesNeeded, firstPart)
				if file.oldName != "" {
					delete(filesNeeded, first(file.oldName))
					delete(filesNeeded, file.Name())
				}
				commitsSinceLastFind = 0 // Reset counter

				// If we found all files, we can stop!
				if len(filesNeeded) == 0 {
					// Kill the git process - we're done
					_ = cmd.Process.Kill()
					// Drain remaining output to avoid broken pipe errors
					go func() {
						for scanner.Scan() {
						}
					}()
					return nil
				}
			}

			// Early exit if we haven't found new files in a while
			if commitsSinceLastFind > giveUpAfter {
				_ = cmd.Process.Kill()
				go func() {
					for scanner.Scan() {
					}
				}()
				return nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		// If we killed the process, we might get an error - that's fine if we found everything
		if len(filesNeeded) == 0 {
			return nil
		}
		return err
	}

	// Wait for process to finish
	_ = cmd.Wait()

	// If some files weren't found, that's okay - they might be very old
	// or not in git history. They'll just have empty commit info.

	return nil
}

// parseGitLogParallel runs git log -1 for each file in parallel and parses
// the results. This is used for deleted files where we need individual lookups.
func parseGitLogParallel(files []*File) {
	results := make(chan gitLogResult, len(files))

	// Launch all git log commands in parallel
	for _, file := range files {
		go func(f *File) {
			cmd := exec.Command("git", "-c", "core.fsmonitor=false", "log", "-1", "--date=format:%Y-%m-%d",
				"--pretty=format:%H%x00%h%x00%ad%x00%aN%x00%aE%x00%s", "--", f.Name())
			out, _ := cmd.Output()
			results <- gitLogResult{file: f, output: out}
		}(file)
	}

	// Collect results
	for range len(files) {
		result := <-results
		if len(result.output) == 0 {
			continue
		}

		parts := strings.SplitN(string(result.output), "\x00", 6)
		if len(parts) != 6 {
			continue
		}

		result.file.hash = parts[0]
		result.file.shortHash = parts[1]
		result.file.lastModified = parts[2]
		result.file.author = parts[3]
		result.file.authorEmail = parts[4]
		result.file.message = parts[5]
	}
}

// first returns the first part of a filepath. Given "some/file/path", it will
// return "some". Modified from golang's built-in Split function:
// https://github.com/golang/go/blob/c5698e315/src/internal/filepathlite/path.go#L204-L212
func first(path string) string {
	i := 0
	for i < len(path) && !os.IsPathSeparator(path[i]) {
		i++
	}
	return path[:i]
}

// diff returns an integer for +/-, or a literal '-' for a binary file. Return
// 0 if the file was binary; we'll just ignore it for diffStat purposes. Is
// there anything better to do with them here?
func diffInt(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return i
}

func gitDiffStat() []byte {
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "diff", "--numstat", "--relative", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		// If HEAD doesn't exist (empty repo with no commits), return empty result
		// rather than crashing. Exit code 128 is git's "fatal error" code.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 128 {
			return []byte{}
		}
		log.Fatalf("Diffstat error: %v", err)
	}
	return output
}

func parseDiffStat(diffStat []byte, files []*File) {
	diffStats := make(map[string][]Diff)
	for line := range strings.SplitSeq(strings.TrimSpace(string(diffStat)), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		plus := diffInt(parts[0])
		minus := diffInt(parts[1])
		path := first(strings.TrimSpace(parts[2]))
		diffStats[path] = append(diffStats[path], Diff{plus, minus})
	}

	for _, file := range files {
		// if the file has any diffs, sum them up. This way we aggregate a
		// directory's diffs
		if stats, ok := diffStats[file.Name()]; ok {
			plus := 0
			minus := 0
			for _, stat := range stats {
				plus += stat.plus
				minus += stat.minus
			}

			file.diffSum = &Diff{plus, minus}
		}
	}
}
