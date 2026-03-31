package main

import (
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
)

const VERSION = "4.0.0"

type Diff struct {
	plus  int
	minus int
}

type File struct {
	entry        os.DirEntry
	name         string // used for deleted files that don't have a DirEntry
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
}

// fetchGitData runs all independent git commands in parallel and returns the results
func fetchGitData() *gitResults {
	results := &gitResults{}
	var wg sync.WaitGroup

	wg.Add(5)

	go func() {
		defer wg.Done()
		results.root = gitRoot()
	}()

	go func() {
		defer wg.Done()
		results.status = gitStatus()
	}()

	go func() {
		defer wg.Done()
		results.diffStat = gitDiffStat()
	}()

	go func() {
		defer wg.Done()
		results.currentBranch = gitCurrentBranch()
	}()

	go func() {
		defer wg.Done()
		results.remotes = gitRemotes()
	}()

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

%s
`, link("https://github.com/llimllib/git-ls", "https://github.com/llimllib/git-ls"))
}

func main() {
	argv := os.Args[1:]
	diffWidth := 4
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
		if strings.HasPrefix(argv[0], "--diffWidth") {
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

	curdir := must(filepath.Rel(gitData.root, must(filepath.Abs("."))))
	fileStatus(gitData.status, files, curdir)
	parseGitLogParallel(files)
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
	showColumns(os.Stdout, maxWidth, files, isGithub(gitData.remotes), must(filepath.Abs(".")), formatColumns)
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

const ansiMarker = '\x1b'

// width returns the printable width of a string in a terminal, by ignoring
// ansi sequences. This version assumes all characters have a width of 1, which
// is not true in general but is true in this program. modified from:
// https://github.com/muesli/ansi/blob/276c6243b/buffer.go#L21
func width(s string) int {
	var n int
	var ansi bool

	for _, c := range s {
		if c == ansiMarker {
			ansi = true
		} else if ansi {
			// @, A-Z, a-z terminate the escape
			if (c >= 0x40 && c <= 0x5a) || (c >= 0x61 && c <= 0x7a) {
				ansi = false
			}
		} else {
			// Just assuming single-width characters is good enough™ in this
			// case
			n += 1
		}
	}

	return n
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
			strings.Repeat("-", plus),
			RESET)
	}
	return fmt.Sprintf("%s%s%s%s%s",
		GREEN,
		strings.Repeat("+", scaleLinear(plus, width, plus+minus)),
		RED,
		strings.Repeat("-", scaleLinear(minus, width, plus+minus)),
		RESET)
}

func showColumns(out io.Writer, maxWidth int, files []*File, githubURL string, dir string, columns []Column) {
	// Calculate max widths for each column
	colWidths := calculateColumnWidths(files, columns)

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
			colWidth := colWidths[col]
			if colWidth > availableWidth {
				colWidth = availableWidth
			}

			// Render the column
			renderer := getColumnRenderer(col)
			renderer(out, file, colWidth, githubURL, dir)
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
	cmd := exec.Command("git", "remote", "-v")
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

func gitCurrentBranch() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git status: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// gitRoot returns the root directory of the git repository
func gitRoot() string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git status: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// gitStatus accepts a dir and a slice of files, and adds the git status to
// each file in place
func gitStatus() []byte {
	cmd := exec.Command("git", "status", "--porcelain", "--ignored")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git status: %v", err)
	}
	return out
}

func fileStatus(status []byte, files []*File, curdir string) {
	gitStatusMap := make(map[string][]string)
	for line := range strings.SplitSeq(string(status), "\n") {
		if len(line) >= 3 {
			status := line[:2]
			// TODO: reject filenames that aren't in the current directory. Can
			// we just ignore ".." entries? Right now, if you're in /subdir,
			// and there's changes in /otherdir/whatever , this will create
			// gitStatusMap entries of "..", which doesn't seem to mess stuff
			// up but isn't ideal either
			fileName := first(must(filepath.Rel(curdir, line[3:])))
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

// parseGitLogParallel runs git log -1 for each file in parallel and parses
// the results. This is faster than sequential calls due to parallelism, and
// faster than a single `git log -- .` because -1 limits to finding just one
// commit per file rather than traversing entire history.
func parseGitLogParallel(files []*File) {
	results := make(chan gitLogResult, len(files))

	// Launch all git log commands in parallel
	for _, file := range files {
		go func(f *File) {
			cmd := exec.Command("git", "log", "-1", "--date=format:%Y-%m-%d",
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
	cmd := exec.Command("git", "diff", "--numstat", "--relative", "HEAD")
	output, err := cmd.Output()
	if err != nil {
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
