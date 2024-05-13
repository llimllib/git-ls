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
	"syscall"
	"unsafe"
)

const VERSION = "3.2.0"

type Diff struct {
	plus  int
	minus int
}

type File struct {
	entry        os.DirEntry
	status       string
	diffSum      *Diff
	diffStat     string
	author       string
	authorEmail  string
	hash         string
	lastModified string
	message      string
	isDir        bool
	isExe        bool
}

const (
	BLUE   = "\x1b[34m"
	GREEN  = "\x1b[32m"
	RED    = "\x1b[31m"
	RESET  = "\x1b[0m"
	YELLOW = "\x1b[33m"
)

func must[T any](a T, e error) T {
	if e != nil {
		log.Fatalf("%v", e)
	}
	return a
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

%s
`, link("https://github.com/llimllib/git-ls", "https://github.com/llimllib/git-ls"))
}

func main() {
	argv := os.Args[1:]
	diffWidth := 4
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
		}
	}

	var dir string
	if len(argv) > 1 {
		dir = argv[1]

		if err := os.Chdir(dir); err != nil {
			log.Fatalf("Failed to change directory to %s: %v", dir, err)
		}
	} else {
		dir = "."
	}

	osfiles, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("Failed to read directory %s: %v", dir, err)
	}

	var files []*File
	for _, file := range osfiles {
		stat, _ := os.Stat(file.Name())
		files = append(files, &File{
			entry: file,
			isDir: file.IsDir(),
			isExe: !file.IsDir() && stat.Mode()&0111 != 0,
		})
	}

	root := gitRoot()
	curdir := must(filepath.Rel(root, must(filepath.Abs("."))))
	fileStatus(gitStatus(), files, curdir)
	parseGitLog(files, gitLog)
	parseDiffStat(gitDiffStat(), files)

	// generate a diffStat graph for every file
	for _, file := range files {
		file.diffStat = makeDiffGraph(file, diffWidth)
	}

	maxWidth := columns(os.Stdout.Fd())
	fmt.Printf("On branch %s%s%s\n\n", RED, gitCurrentBranch(), RESET)
	show(os.Stdout, maxWidth, files, isGithub(gitRemotes()), must(filepath.Abs(dir)))
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
		commitUrl := fmt.Sprintf("%s/commit/%s", github, hash)
		out = append(out, link(commitUrl, commitMsg[:issueIx[0]]))

		issueUrl := fmt.Sprintf("%s/pull/%s", github, commitMsg[issueIx[0]+1:issueIx[1]])
		issueText := fmt.Sprintf("%s%s%s", BLUE, commitMsg[issueIx[0]:issueIx[1]], RESET)
		out = append(out, link(issueUrl, issueText))

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
func scale_linear(n int, width int, max_change int) int {
	if n == 0 {
		return 0
	}
	/*
	 * make sure that at least one '-' or '+' is printed if
	 * there is any change to this path. The easiest way is to
	 * scale linearly as if the allotted width is one column shorter
	 * than it is, and then add 1 to the result.
	 */
	return 1 + (n * (width - 1) / max_change)
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
		strings.Repeat("+", scale_linear(plus, width, plus+minus)),
		RED,
		strings.Repeat("-", scale_linear(minus, width, plus+minus)),
		RESET)
}

func show(out io.Writer, maxWidth int, files []*File, githubUrl string, dir string) {
	maxStatus := 0
	maxDiffStat := 0
	maxNameLen := 0
	for _, file := range files {
		if len(file.status) > maxStatus {
			maxStatus = len(file.status)
		}
		if width(file.diffStat) > maxDiffStat {
			maxDiffStat = width(file.diffStat)
		}
		if len(file.entry.Name()) > maxNameLen {
			maxNameLen = len(file.entry.Name())
		}
	}

	for _, file := range files {
		// lineWidth tracks the width of the current line
		lineWidth := 0

		// print the file's git status. If there are no modified files, skip entirely
		if maxStatus > 0 {
			fmt.Fprintf(out, fmt.Sprintf("%%%ds ", maxStatus), file.status)
			lineWidth += maxStatus + 1

			// print the diffstat summary for the file
			fmt.Fprintf(out, "%s", file.diffStat)
			for i := 0; i < maxDiffStat-width(file.diffStat)+1; i++ {
				fmt.Fprintf(out, " ")
			}
			lineWidth += 5
		}

		if file.isDir {
			fmt.Fprintf(out, "%s", BLUE)
		}
		if file.isExe {
			fmt.Fprintf(out, "%s", GREEN)
		}
		// link the file name to the file's location
		fileUrl := fmt.Sprintf("file://%s%s", must(os.Hostname()), filepath.Join(dir, file.entry.Name()))
		fmt.Fprintf(out, "%s", link(fileUrl, file.entry.Name()))
		// pad spaces to the right up to maxNameLen
		for i := 0; i < maxNameLen-len(file.entry.Name()); i++ {
			fmt.Fprintf(out, " ")
		}
		if file.isDir || file.isExe {
			fmt.Fprintf(out, "%s", RESET)
		}
		lineWidth += maxNameLen

		// write the last modified date
		fmt.Fprintf(out, " %s", file.lastModified)
		lineWidth += len(file.lastModified) + 1

		if lineWidth >= maxWidth {
			fmt.Println("")
			continue
		}
		authorWidth := min(len(file.author), maxWidth-1-lineWidth)
		lineWidth += authorWidth + 1
		if len(githubUrl) > 0 {
			// if this is a github repo, link the author name to their commits
			// page on github. It would be cool to hyperlink the author to
			// a git command, but I'm not sure how to give a URL for the command
			// `git log --author=Janet`
			authorLink := fmt.Sprintf("%s/commits?author=%s", githubUrl, file.authorEmail)
			fmt.Fprintf(out, " %s%s%s", YELLOW, link(authorLink, file.author[:authorWidth]), RESET)
		} else {
			fmt.Fprintf(out, " %s%s%s", YELLOW, file.author[:authorWidth], RESET)
		}

		// If this is a github repo, look for #<issue> links and linkify them.
		// Otherwise just output the first 80 chars of the commit msg. Would it
		// be better to use the full width of the terminal if available here,
		// or just keep it shortish?
		if lineWidth >= maxWidth {
			fmt.Println("")
			continue
		}
		messageWidth := min(len(file.message), maxWidth-1-lineWidth)
		if len(githubUrl) > 0 {
			fmt.Fprintf(out, " %s\n", linkify(file.message[:messageWidth], githubUrl, file.hash))
		} else {
			fmt.Fprintf(out, " %s\n", file.message[:messageWidth])
		}
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
	lines := strings.Split(string(status), "\n")

	for _, line := range lines {
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
		if fileStatus, ok := gitStatusMap[file.entry.Name()]; ok {
			slices.Sort(fileStatus)
			file.status = strings.Join(slices.Compact(fileStatus), ",")
		}
		if file.entry.Name() == ".git" {
			file.status = "*"
		}
	}
}

func gitLog(file *File) []byte {
	cmd := exec.Command("git", "log", "-1", "--date=format:%Y-%m-%d",
		"--pretty=format:%h%x00%ad%x00%aN%x00%aE%x00%s", "--", file.entry.Name())
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git info for file %s: %v", file.entry.Name(), err)
	}
	return out
}

func parseGitLog(files []*File, gitLog func(file *File) []byte) {
	for _, file := range files {
		out := gitLog(file)

		if len(out) == 0 {
			continue
		}

		parts := strings.SplitN(string(out), "\x00", 5)
		if len(parts) != 5 {
			log.Fatalf("unexpected output format: %#v", out)
		}

		file.hash = parts[0]
		file.lastModified = parts[1]
		file.author = parts[2]
		file.authorEmail = parts[3]
		file.message = parts[4]
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
	lines := strings.Split(strings.TrimSpace(string(diffStat)), "\n")
	for _, line := range lines {
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
		if stats, ok := diffStats[file.entry.Name()]; ok {
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
