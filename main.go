package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const VERSION = "1.2.0"

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

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("%s\n", VERSION)
		os.Exit(0)
	}

	var dir string
	if len(os.Args) > 1 {
		dir = os.Args[1]

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

	gitStatus(files)
	gitLog(files)
	gitDiffStat(files)

	// generate a diffStat graph for every file
	for _, file := range files {
		// eventually I'd probably like to make width a flag. For now,
		// width == 4
		file.diffStat = makeDiffGraph(file, 4)
	}

	maxWidth := columns(os.Stdout.Fd())
	fmt.Printf("On branch %s%s%s\n\n", RED, gitCurrentBranch(), RESET)
	show(os.Stdout, maxWidth, files, isGithub())
}

func link(url string, name string) string {
	// hyperlink format: \e]8;;<url>\e\<link text>\e]8;;\e\
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, name)
}

func linkify(s string, github string) string {
	issue_re := regexp.MustCompile(`#(\d+)`)

	// Function to replace matches with OSC8 hyperlinks
	replaceFunc := func(match string) string {
		issueNumber, _ := strconv.Atoi(match[1:])
		// I'm not sure how to link to _either_ a PR or an issue. Is there a
		// URL that I can use that will automatically go to the appropriate
		// place?
		return link(fmt.Sprintf("%s/pull/%d", github, issueNumber), match)
	}

	// Replace all matches with OSC8 hyperlinks
	output := issue_re.ReplaceAllStringFunc(s, replaceFunc)

	return output
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

func show(out io.Writer, maxWidth int, files []*File, githubUrl string) {
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
		fmt.Fprintf(out, "%s", link("file:"+file.entry.Name(), file.entry.Name()))
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
			fmt.Fprintf(out, " %s\n", linkify(file.message[:messageWidth], githubUrl))
		} else {
			fmt.Fprintf(out, " %s\n", file.message[:messageWidth])
		}
	}
}

func isGithub() string {
	cmd := exec.Command("git", "remote", "-v")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git status: %v", err)
	}

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

// gitStatus accepts a dir and a slice of files, and adds the git status to
// each file in place
func gitStatus(files []*File) {
	cmd := exec.Command("git", "status", "--porcelain", "--ignored")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git status: %v", err)
	}

	gitStatusMap := make(map[string][]string)
	lines := strings.Split(string(out), "\n")

	for _, line := range lines {
		if len(line) >= 3 {
			status := line[:2]
			fileName := first(line[3:])
			if status == "!!" {
				status = "I"
			}
			gitStatusMap[fileName] = append(gitStatusMap[fileName], status)
		}
	}

	for _, file := range files {
		if fileStatus, ok := gitStatusMap[file.entry.Name()]; ok {
			file.status = strings.Join(slices.Compact(fileStatus), ",")
		}
		if file.entry.Name() == ".git" {
			file.status = "*"
		}
	}
}

func gitLog(files []*File) {
	for _, file := range files {
		cmd := exec.Command("git", "log", "-1", "--date=format:%Y-%m-%d", "--pretty=format:%h|%ad|%aN|%aE|%s", "--", file.entry.Name())
		out, err := cmd.Output()
		if err != nil {
			log.Fatalf("Failed to get git info for file %s: %v", file.entry.Name(), err)
		}

		if len(out) == 0 {
			continue
		}

		parts := strings.SplitN(string(out), "|", 5)
		if len(parts) != 5 {
			log.Fatalf("unexpected output format: %s", out)
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

func gitDiffStat(files []*File) {
	cmd := exec.Command("git", "diff", "--numstat", "--relative", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		log.Fatalf("Diffstat error: %v", err)
	}

	diffStats := make(map[string][]Diff)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
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
