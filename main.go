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
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const VERSION = "7.1.2"

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
	commitTime   int64 // unix timestamp of the commit for precise sorting
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

// DebugTimer tracks timing information for debug mode
type DebugTimer struct {
	timings     map[string]time.Duration
	fileTimings map[string]int // stores line counts for file lookups
	mu          sync.Mutex
}

func newDebugTimer() *DebugTimer {
	return &DebugTimer{
		timings:     make(map[string]time.Duration),
		fileTimings: make(map[string]int),
	}
}

func (dt *DebugTimer) time(name string, fn func()) {
	start := time.Now()
	fn()
	elapsed := time.Since(start)
	dt.mu.Lock()
	dt.timings[name] = elapsed
	dt.mu.Unlock()
}

func (dt *DebugTimer) record(name string, duration time.Duration) {
	dt.mu.Lock()
	dt.timings[name] = duration
	dt.mu.Unlock()
}

func (dt *DebugTimer) print() {
	fmt.Fprintf(os.Stderr, "\nDebug Timings:\n")

	// Define the order we want to print top-level timings
	topLevelOrder := []string{
		"fetchGitData",
		"filesFromCurDir",
		"changedFilesFromStatus",
		"parseGitLog",
		"parseDiffStat",
		"makeDiffGraph",
		"showColumns",
	}

	// Print each top-level timing
	for _, name := range topLevelOrder {
		if duration, ok := dt.timings[name]; ok {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", name, duration)
			// If this is fetchGitData, print its nested timings
			if name == "fetchGitData" {
				for childName, childDuration := range dt.timings {
					if stripped, ok := strings.CutPrefix(childName, "  "); ok {
						fmt.Fprintf(os.Stderr, "    %s: %v\n", stripped, childDuration)
					}
				}
			}
		}
	}
}

// printDebugFileInfo prints debug information about file processing
func printDebugFileInfo(timer *DebugTimer, files []*File) {
	// Find the file that took the longest to find in git log
	var slowestFile string
	var slowestLines int
	for fileName, lines := range timer.fileTimings {
		if lines > slowestLines {
			slowestFile = fileName
			slowestLines = lines
		}
	}

	if slowestFile != "" {
		fmt.Fprintf(os.Stderr, "\nSlowest file to find: %s (%d lines)\n", slowestFile, slowestLines)
	}

	// Check for files that weren't found (no hash means not found in history)
	var notFound []string
	for _, file := range files {
		if file.hash == "" {
			notFound = append(notFound, file.Name())
		}
	}

	if len(notFound) > 0 {
		fmt.Fprintf(os.Stderr, "\nWarning: %d file(s) not found in git history (possibly beyond %s commit limit):\n", len(notFound), HistoryLimit)
		for _, fileName := range notFound {
			fmt.Fprintf(os.Stderr, "  - %s\n", fileName)
		}
	}
}

// Name returns the file name, either from the DirEntry or the name field
func (f *File) Name() string {
	if f.entry != nil {
		return f.entry.Name()
	}
	return f.name
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
	lsFiles       []byte // tracked files from git ls-files
	isEmpty       bool   // true if repo has no commits yet
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
func fetchGitData(timer *DebugTimer) *gitResults {
	results := &gitResults{}

	var wg sync.WaitGroup

	wg.Go(func() {
		start := time.Now()
		results.root = gitRoot()
		if timer != nil {
			timer.record("  gitRoot", time.Since(start))
		}
	})

	wg.Go(func() {
		start := time.Now()
		results.status = gitStatus()
		if timer != nil {
			timer.record("  gitStatus", time.Since(start))
		}
	})

	wg.Go(func() {
		start := time.Now()
		results.currentBranch = headDescription()
		if timer != nil {
			timer.record("  headDescription", time.Since(start))
		}
	})

	wg.Go(func() {
		start := time.Now()
		results.remotes = gitRemotes()
		if timer != nil {
			timer.record("  gitRemotes", time.Since(start))
		}
	})

	wg.Go(func() {
		start := time.Now()
		results.lsFiles = gitLsFiles()
		if timer != nil {
			timer.record("  gitLsFiles", time.Since(start))
		}
	})

	// No diff stats in empty repo
	if isEmptyRepo() {
		results.isEmpty = true
		results.diffStat = []byte{}
	} else {
		wg.Go(func() {
			start := time.Now()
			results.diffStat = gitDiffStat()
			if timer != nil {
				timer.record("  gitDiffStat", time.Since(start))
			}
		})
	}

	wg.Wait()
	return results
}

// printVersion prints version info including the commit hash if available
func printVersion() {
	commit := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				commit = setting.Value
				if len(commit) > 7 {
					commit = commit[:7]
				}
				break
			}
		}
	}
	if commit != "" {
		fmt.Printf("%s (%s)\n", VERSION, commit)
	} else {
		fmt.Printf("%s\n", VERSION)
	}
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

    -c, --changed-only
        Only show files that have changes (appear in git status). This
        filters out unmodified tracked files and shows only files with
        status indicators (modified, added, deleted, renamed, etc.)

    -s, --sort
        Sort files by last git modification date, most recent first.
        Files without a date (ignored, untracked, .git) are placed at
        the bottom in alphabetical order.

    --debug
        Print debug timing information to stderr, including which file
        took the longest to find in git history and warnings for files
        not found (possibly beyond the history limit)

%s
`, link("https://github.com/llimllib/git-ls", "https://github.com/llimllib/git-ls"))
}

func main() {
	os.Exit(run())
}

func run() int {
	argv := os.Args[1:]
	diffWidth := 4
	monoHash := false
	nerdFont := false
	changedOnly := false
	sortByDate := false
	debug := false
	var formatColumns []Column
	for len(argv) > 0 {
		if argv[0] == "--version" {
			printVersion()
			return 0
		}
		if argv[0] == "--help" || argv[0] == "-h" {
			usage()
			return 0
		}
		if argv[0] == "--mono-hash" {
			monoHash = true
			argv = argv[1:]
		} else if argv[0] == "--nerdfont" {
			nerdFont = true
			argv = argv[1:]
		} else if argv[0] == "--changed-only" || argv[0] == "-c" {
			changedOnly = true
			argv = argv[1:]
		} else if argv[0] == "--sort" || argv[0] == "-s" {
			sortByDate = true
			argv = argv[1:]
		} else if strings.HasPrefix(argv[0], "--diffWidth") {
			if len(argv) == 1 {
				if strings.Contains(argv[0], "=") {
					parts := strings.SplitN(argv[0], "=", 2)
					diffWidth = must(strconv.Atoi(parts[1]))
				} else {
					fmt.Fprintf(os.Stderr, "--diffWidth requires an argument\n")
					return 1
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
					fmt.Fprintf(os.Stderr, "--format requires an argument\n")
					return 1
				}
				argv = argv[1:]
			} else {
				formatColumns = parseFormat(argv[1])
				argv = argv[2:]
			}
		} else if strings.HasPrefix(argv[0], "--debug") {
			debug = true
			argv = argv[1:]
		} else {
			// Non-flag argument (directory), stop parsing flags
			break
		}
	}

	var dir string
	if len(argv) > 0 {
		dir = argv[0]

		if err := os.Chdir(dir); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to change directory to %s: %v\n", dir, err)
			return 1
		}
	} else {
		dir = "."
	}

	// Fetch all git data in parallel
	var timer *DebugTimer
	if debug {
		timer = newDebugTimer()
	}

	var gitData *gitResults
	if debug {
		timer.time("fetchGitData", func() {
			gitData = fetchGitData(timer)
		})
	} else {
		gitData = fetchGitData(nil)
	}

	// Resolve symlinks to match git's perspective. Git internally resolves
	// symlinks when working with worktrees, so we need to do the same to
	// ensure filepath.Rel() works correctly in fileStatus().
	// https://github.com/llimllib/git-ls/issues/34
	resolved := must(filepath.EvalSymlinks(must(filepath.Abs("."))))
	curdir := must(filepath.Rel(gitData.root, resolved))

	var files []*File
	if changedOnly {
		// In --changed-only mode, build the file list directly from git
		// status so we can show files from subdirectories with their full
		// relative paths (e.g. "src/network/server.go" not just "src/")
		if debug {
			timer.time("changedFilesFromStatus", func() {
				files = changedFilesFromStatus(gitData.status, curdir)
			})
		} else {
			files = changedFilesFromStatus(gitData.status, curdir)
		}
	} else {
		var err error
		if debug {
			timer.time("filesFromCurDir", func() {
				files, err = filesFromCurDir(dir, gitData, curdir)
			})
		} else {
			files, err = filesFromCurDir(dir, gitData, curdir)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
	}

	// Now run git log on the files we'll show
	// Skip git log for files that won't have history:
	// - I: Ignored files - not tracked by git
	// - ??: Untracked files - not in git yet
	// - *: .git directory - git metadata, not file history
	// - A: Newly added files
	//   Examples: "A ", "AM", "AD" - staged additions not yet committed
	//   But "M ", "MM", or mixed dirs like "A ,M " have history
	var filesNeedingLog []*File
	for _, file := range files {
		if file.status == "I" || file.status == "??" || file.status == "*" {
			continue
		}
		// Empty status means unmodified tracked file — it has history
		if file.status == "" {
			filesNeedingLog = append(filesNeedingLog, file)
			continue
		}
		// Check if ALL statuses in a comma-separated list are new additions
		// For directories, status might be "A ,M " if it has both new and modified files
		allNew := true
		for status := range strings.SplitSeq(file.status, ",") {
			status = strings.TrimSpace(status)
			// Check if this individual status represents a file with history
			// If first char (index) is not 'A' and not untracked, it has history
			if len(status) > 0 && status[0] != 'A' && status != "??" {
				allNew = false
				break
			}
		}
		if !allNew {
			filesNeedingLog = append(filesNeedingLog, file)
		}
	}

	if debug {
		timer.time("parseGitLog", func() {
			if err := parseGitLog(filesNeedingLog, timer); err != nil {
				fmt.Fprintf(os.Stderr, "Error: git log streaming failed: %v\n", err)
			}
		})
		timer.time("parseDiffStat", func() {
			parseDiffStat(gitData.diffStat, files)
		})
	} else {
		if err := parseGitLog(filesNeedingLog, nil); err != nil {
			fmt.Fprintf(os.Stderr, "Error: git log streaming failed: %v\n", err)
			return 1
		}
		parseDiffStat(gitData.diffStat, files)
	}

	// generate a diffStat graph for every file
	if debug {
		timer.time("makeDiffGraph", func() {
			for _, file := range files {
				file.diffStat = makeDiffGraph(file, diffWidth)
			}
		})
	} else {
		for _, file := range files {
			file.diffStat = makeDiffGraph(file, diffWidth)
		}
	}

	// Sort files
	if sortByDate {
		sortFilesByDate(files)
	} else {
		sortFilesByName(files)
	}

	// Use default columns if not specified
	if len(formatColumns) == 0 {
		formatColumns = AllColumns()
	}

	maxWidth := terminalColumns(os.Stdout.Fd())
	if maxWidth == 0 {
		maxWidth = 80 // default when not a TTY
	}
	fmt.Printf("%s%s%s\n\n", RED, gitData.currentBranch, RESET)
	rctx := &RenderContext{
		GithubURL: isGithub(gitData.remotes),
		Dir:       must(filepath.Abs(".")),
		MonoHash:  monoHash,
		NerdFont:  nerdFont,
	}
	if debug {
		timer.time("showColumns", func() {
			showColumns(os.Stdout, maxWidth, files, rctx, formatColumns)
		})
		timer.print()
		printDebugFileInfo(timer, filesNeedingLog)
	} else {
		showColumns(os.Stdout, maxWidth, files, rctx, formatColumns)
	}

	// Print total +/- summary if there are any diffs
	if nFiles, totalPlus, totalMinus := totalDiffStats(files); totalPlus > 0 || totalMinus > 0 {
		fileWord := "files"
		if nFiles == 1 {
			fileWord = "file"
		}
		fmt.Printf("\n%d %s %s+%d%s %s-%d%s\n", nFiles, fileWord, GREEN, totalPlus, RESET, RED, totalMinus, RESET)
	}

	return 0
}

// changedFilesFilter strips unchanged files from the files to display
func changedFilesFilter(files []*File) []*File {
	var changedFiles []*File
	for _, file := range files {
		if file.status != "" && file.status != "I" && file.status != "*" {
			changedFiles = append(changedFiles, file)
		}
	}
	return changedFiles
}

// filesFromCurDir lists the files in the current directory. `dir` is the
// directory as the user specified it, and `curdir` is the directory as we
// resolved it from the git root
func filesFromCurDir(dir string, gitData *gitResults, curdir string) ([]*File, error) {
	var files []*File
	osfiles, err := os.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %v", dir, err)
	}

	for _, file := range osfiles {
		stat, _ := os.Stat(file.Name())
		files = append(files, &File{
			entry: file,
			isDir: file.IsDir(),
			isExe: !file.IsDir() && stat.Mode()&0o111 != 0,
		})
	}

	fileStatus(gitData.status, gitData.lsFiles, files, curdir)

	// Parse deleted files from git status and merge into file list
	deletedFiles := parseDeletedFiles(gitData.status, curdir)
	return append(files, deletedFiles...), nil
}

// changedFilesFromStatus builds a list of changed File structs directly from
// git status output. Unlike the normal flow (which reads the current directory
// and then filters), this produces files with full relative paths so that
// --changed-only can show files from subdirectories.
func changedFilesFromStatus(status []byte, curdir string) []*File {
	var files []*File
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(string(status), "\n") {
		if len(line) < 3 {
			continue
		}
		statusCode := line[:2]
		path := line[3:]

		// Skip ignored files
		if statusCode == "!!" {
			continue
		}

		// For renames/copies, use the new name and record the old name
		var oldName string
		if (statusCode[0] == 'R' || statusCode[0] == 'C') && strings.Contains(path, " -> ") {
			parts := strings.SplitN(path, " -> ", 2)
			oldName = must(filepath.Rel(curdir, parts[0]))
			path = parts[1]
		}

		relPath := must(filepath.Rel(curdir, path))

		// Skip files outside the current directory (would start with "..")
		if strings.HasPrefix(relPath, "..") {
			continue
		}

		// Deduplicate (a file can appear in status with different codes)
		if seen[relPath] {
			continue
		}
		seen[relPath] = true

		isDeleted := statusCode == " D" || statusCode == "D "
		isExe := false
		if !isDeleted {
			if stat, err := os.Stat(relPath); err == nil {
				isExe = !stat.IsDir() && stat.Mode()&0o111 != 0
			}
		}

		file := &File{
			name:      relPath,
			status:    statusCode,
			oldName:   oldName,
			isDeleted: isDeleted,
			isExe:     isExe,
		}
		files = append(files, file)
	}
	return files
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
	scaledPlus := scaleLinear(plus, width, plus+minus)
	scaledMinus := scaleLinear(minus, width, plus+minus)
	// scaleLinear guarantees at least 1 for non-zero values, so the sum
	// can exceed width. Cap the total to width, preserving the ratio.
	if scaledPlus+scaledMinus > width {
		scaledMinus = width - scaledPlus
	}
	return fmt.Sprintf("%s%s%s%s%s",
		GREEN,
		strings.Repeat("+", scaledPlus),
		RED,
		strings.Repeat("-", scaledMinus),
		RESET)
}

// sortFilesByDate sorts files by commit timestamp descending (most recent first).
// Files without a timestamp (commitTime == 0) go to the bottom, sorted alphabetically.
func sortFilesByDate(files []*File) {
	slices.SortStableFunc(files, func(a, b *File) int {
		if a.commitTime == 0 && b.commitTime == 0 {
			return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
		}
		if a.commitTime == 0 {
			return 1
		}
		if b.commitTime == 0 {
			return -1
		}
		if b.commitTime != a.commitTime {
			return int(b.commitTime - a.commitTime)
		}
		return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
	})
}

// sortFilesByName sorts files alphabetically by name (case-insensitive).
func sortFilesByName(files []*File) {
	slices.SortFunc(files, func(a, b *File) int {
		return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
	})
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

			// Check if there's room for another column after this one.
			// If not, we shouldn't pad this column to avoid trailing spaces.
			isLastColumn := (i == len(columns)-1) || (lineWidth+colWidth >= maxWidth)
			if !isLastColumn && i+1 < len(columns) {
				// Check if next column would fit (need space separator + min 1 char)
				isLastColumn = (lineWidth+colWidth+1 >= maxWidth)
			}

			// Render the column
			renderer := getColumnRenderer(col)
			renderer(out, file, colWidth, rctx, isLastColumn)
			lineWidth += colWidth

			// If this was the last column that fits, stop rendering
			if isLastColumn {
				break
			}
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

// headDescription returns a string describing the current HEAD state,
// matching git-status output: "On branch X", "HEAD detached at X",
// "HEAD detached from X", or rebase-in-progress messages.
// The returned string includes the prefix (e.g. "On branch ").
func headDescription() string {
	// Try symbolic-ref first — if it works, we're on a branch
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "symbolic-ref", "--short", "HEAD")
	out, err := cmd.Output()
	if err == nil {
		return "On branch " + strings.TrimSpace(string(out))
	}

	// We're in detached HEAD state. Check for rebase.
	gitDir := gitCommonDir()
	if desc := rebaseDescription(gitDir); desc != "" {
		return desc
	}

	// Determine "detached at" vs "detached from" by comparing HEAD to the
	// commit we originally detached at (found via reflog).
	return detachedDescription()
}

// gitCommonDir returns the path to the git common dir (handles worktrees).
func gitCommonDir() string {
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return ".git"
	}
	return strings.TrimSpace(string(out))
}

// rebaseDescription checks for an in-progress rebase and returns the
// appropriate status string, or "" if no rebase is active.
func rebaseDescription(gitDir string) string {
	ontoBytes, err := os.ReadFile(filepath.Join(gitDir, "rebase-merge", "onto"))
	if err == nil {
		onto := strings.TrimSpace(string(ontoBytes))
		onto = shortOid(onto)
		if _, e := os.Stat(filepath.Join(gitDir, "rebase-merge", "interactive")); e == nil {
			return "interactive rebase in progress; onto " + onto
		}
		return "rebase in progress; onto " + onto
	}
	ontoBytes, err = os.ReadFile(filepath.Join(gitDir, "rebase-apply", "onto"))
	if err == nil {
		onto := strings.TrimSpace(string(ontoBytes))
		onto = shortOid(onto)
		return "rebase in progress; onto " + onto
	}
	return ""
}

// shortOid abbreviates a full hex oid via rev-parse --short.
func shortOid(oid string) string {
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "rev-parse", "--short", oid)
	out, err := cmd.Output()
	if err != nil {
		return oid
	}
	return strings.TrimSpace(string(out))
}

// detachedDescription returns "HEAD detached at X" or "HEAD detached from X"
// by inspecting the reflog, mirroring git-status behavior.
func detachedDescription() string {
	// Walk the reflog to find the checkout/switch entry where we detached.
	// That entry's oid is the commit we originally landed on.
	cmd := exec.Command("git", "-c", "core.fsmonitor=false",
		"reflog", "--format=%H %gs", "HEAD")
	reflogOut, err := cmd.Output()
	if err != nil {
		return "Not currently on any branch."
	}

	var detachedOid string
	for line := range strings.SplitSeq(strings.TrimSpace(string(reflogOut)), "\n") {
		if strings.Contains(line, "checkout: ") || strings.Contains(line, "switch: ") {
			detachedOid, _, _ = strings.Cut(line, " ")
			break
		}
	}
	if detachedOid == "" {
		return "Not currently on any branch."
	}

	// Get current HEAD oid
	cmd = exec.Command("git", "-c", "core.fsmonitor=false", "rev-parse", "HEAD")
	headOut, err := cmd.Output()
	if err != nil {
		return "Not currently on any branch."
	}
	headOid := strings.TrimSpace(string(headOut))

	atOrFrom := "at"
	if headOid != detachedOid {
		atOrFrom = "from"
	}

	// Try to find a friendly name (tag or branch) that points at detachedOid
	name := friendlyRefName(detachedOid)

	return "HEAD detached " + atOrFrom + " " + name
}

// friendlyRefName tries to describe an oid with a tag or branch name.
// Falls back to the short hash.
func friendlyRefName(oid string) string {
	// Try describe --tags --exact-match first for tags
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "describe", "--tags", "--exact-match", oid)
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	// Try branch name via name-rev
	cmd = exec.Command("git", "-c", "core.fsmonitor=false", "name-rev", "--name-only", "--no-undefined", "--refs=refs/heads/*", oid)
	out, err = cmd.Output()
	if err == nil {
		name := strings.TrimSpace(string(out))
		if name != "" && !strings.Contains(name, "~") && !strings.Contains(name, "^") {
			return name
		}
	}
	return shortOid(oid)
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
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git status: %v", err)
	}
	return out
}

// gitLsFiles returns all tracked files
func gitLsFiles() []byte {
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "ls-files")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git ls-files: %v", err)
	}
	return out
}

func fileStatus(status []byte, lsFiles []byte, files []*File, curdir string) {
	// Build map of tracked files (from git ls-files)
	// Note: git ls-files is run from the current directory, so paths are relative to cwd
	trackedFiles := make(map[string]bool)
	for line := range strings.SplitSeq(string(lsFiles), "\n") {
		if line != "" {
			// git ls-files returns paths relative to current directory
			// For files in subdirectories, we only care about the first component
			// e.g. "subdir/file.txt" -> "subdir"
			fileName := first(line)
			trackedFiles[fileName] = true
		}
	}

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
			gitStatusMap[fileName] = append(gitStatusMap[fileName], status)
		}
	}

	for _, file := range files {
		if fileStatus, ok := gitStatusMap[file.Name()]; ok {
			slices.Sort(fileStatus)
			file.status = strings.Join(slices.Compact(fileStatus), ",")
		} else if !trackedFiles[file.Name()] && file.Name() != ".git" {
			// File exists in filesystem but is not tracked and not in status = ignored
			file.status = "I"
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
				// Get the full relative path first
				relPath := must(filepath.Rel(curdir, line[3:]))
				// Only include files directly in current directory
				// (not ".." for parent dirs, and no path separators for subdirs)
				if relPath != ".." && !strings.Contains(relPath, string(os.PathSeparator)) {
					deletedFiles = append(deletedFiles, &File{
						name:      relPath,
						status:    statusCode,
						isDeleted: true,
					})
				}
			}
		}
	}
	return deletedFiles
}

// parseGitLog runs a single git log command and streams the output,
// stopping as soon as all files are found. This is much faster than spawning
// N processes because:
// 1. Single process (no spawn overhead)
// 2. Early exit (stops when all files found)
// 3. Walks history once (all files benefit from git's caching)
// 4. Directory scoped (only checks current directory)
func parseGitLog(files []*File, timer *DebugTimer) error {
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
		"--format=%H%x00%h%x00%aN%x00%aE%x00%s%x00%at%x00",
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
		author      string
		authorEmail string
		message     string
		timestamp   int64
	}

	// Track lines since last find for early exit. This counts lines of output
	// (both commit metadata and filenames), not commits. In active monorepos
	// a single root-level file might not appear for 100k+ lines if
	// subdirectories see heavy churn.
	linesSinceLastFind := 0
	const giveUpAfter = 150000

	for scanner.Scan() {
		linesSinceLastFind++
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
				currentCommit.author = parts[2]
				currentCommit.authorEmail = parts[3]
				currentCommit.message = parts[4]
				currentCommit.timestamp, _ = strconv.ParseInt(parts[5], 10, 64)
			}
		} else if currentCommit.hash != "" {
			// This is a filename line
			filename := strings.TrimSpace(line)

			// Try to match the full path first (for --changed-only files
			// with relative paths like "src/network/server.go"), then
			// fall back to the first component (for directory-level
			// matching in normal mode, e.g. "builtin/add.c" -> "builtin")
			var file *File
			var matchKey string
			if f, ok := filesNeeded[filename]; ok {
				file = f
				matchKey = filename
			} else {
				firstPart := first(filename)
				if f, ok := filesNeeded[firstPart]; ok {
					file = f
					matchKey = firstPart
				}
			}

			if file != nil {
				// Found it! Populate the file info
				file.hash = currentCommit.hash
				file.shortHash = currentCommit.shortHash
				file.commitTime = currentCommit.timestamp
				file.lastModified = time.Unix(currentCommit.timestamp, 0).UTC().Format("2006-01-02")
				file.author = currentCommit.author
				file.authorEmail = currentCommit.authorEmail
				file.message = currentCommit.message

				// Track timing for this file if in debug mode
				if timer != nil {
					timer.mu.Lock()
					timer.fileTimings[file.Name()] = linesSinceLastFind
					timer.mu.Unlock()
				}

				// Remove from needed set. For renamed/copied files we
				// may have two keys (old name + new name) pointing to
				// the same File, so delete both.
				delete(filesNeeded, matchKey)
				if file.oldName != "" {
					delete(filesNeeded, first(file.oldName))
					delete(filesNeeded, file.Name())
				}
				linesSinceLastFind = 0 // Reset counter

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
			if linesSinceLastFind > giveUpAfter {
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

// totalDiffStats sums up the +/- across all files that have diff information
// and returns the count of files with diffs, total additions, and total deletions.
func totalDiffStats(files []*File) (int, int, int) {
	nFiles := 0
	totalPlus := 0
	totalMinus := 0
	for _, file := range files {
		if file.diffSum != nil {
			nFiles++
			totalPlus += file.diffSum.plus
			totalMinus += file.diffSum.minus
		}
	}
	return nFiles, totalPlus, totalMinus
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
		fullPath := strings.TrimSpace(parts[2])
		d := Diff{plus, minus}
		// Store under both the full relative path (for --changed-only) and
		// the first component (for directory-level aggregation in normal mode)
		diffStats[fullPath] = append(diffStats[fullPath], d)
		if firstPart := first(fullPath); firstPart != fullPath {
			diffStats[firstPart] = append(diffStats[firstPart], d)
		}
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
