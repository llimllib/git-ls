package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

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

func main() {
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
		file.diffStat = makeDiffGraph(file)
	}

	show(dir, files, isGithub())
}

func link(url string, name string) string {
	// hyperlink format: \e]8;;<url>\e\<link text>\e]8;;\e\
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, name)
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

const (
	BLUE  = "\033[34m"
	GREEN = "\033[32m"
	RED   = "\033[31m"
	RESET = "\033[0m"
)

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

// makeDiffGraph turns the total diff for a file/directory into a diff graph
// string. Currently, this function is terrible. TODO: Copy the git logic for
// this, and make diff width configurable maybe:
// https://github.com/git/git/blob/d4cc1ec3/diff.c#L2862-L2874
func makeDiffGraph(file *File) string {
	if file.diffSum == nil {
		return ""
	}
	plus := file.diffSum.plus
	minus := file.diffSum.minus
	if plus == 0 && minus == 0 {
		return ""
	}
	if plus == 0 {
		return fmt.Sprintf("%s----%s", RED, RESET)
	}
	if minus == 0 {
		return fmt.Sprintf("%s++++%s", GREEN, RESET)
	}
	if plus > minus {
		return fmt.Sprintf("%s+++%s-%s", GREEN, RED, RESET)
	}
	if plus == minus {
		return fmt.Sprintf("%s++%s--%s", GREEN, RED, RESET)
	}
	if plus < minus {
		return fmt.Sprintf("%s+%s---%s", GREEN, RED, RESET)
	}
	panic("not possible")
}

func show(dir string, files []*File, githubUrl string) {
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
	// We have to calculate the file name's format separately, because it
	// contains a big escaped hyperlink that printf won't format properly
	fileNameFmt := "%-" + strconv.Itoa(maxNameLen) + "s"
	for _, file := range files {
		// print the file's git status
		fmt.Fprintf(os.Stdout, fmt.Sprintf("%%%ds ", maxStatus), file.status)
		// print the diffstat summary for the file
		fmt.Fprintf(os.Stdout, "%s", file.diffStat)
		for i := 0; i < maxDiffStat-width(file.diffStat)+1; i++ {
			fmt.Fprintf(os.Stdout, " ")
		}
		if file.isDir {
			os.Stdout.WriteString(BLUE)
		}
		if file.isExe {
			os.Stdout.WriteString(GREEN)
		}
		// link the file name to the file's location
		os.Stdout.WriteString(link(
			"file:"+dir+"/"+file.entry.Name(),
			fmt.Sprintf(fileNameFmt, file.entry.Name())))
		if file.isDir || file.isExe {
			os.Stdout.WriteString(RESET)
		}
		fmt.Fprintf(os.Stdout, " %s ", file.lastModified)

		if len(githubUrl) > 0 {
			// if this is a github repo, link the author name to their commits
			// page on github
			authorLink := fmt.Sprintf("%s/commits?author=%s", githubUrl, file.authorEmail)
			fmt.Fprintf(os.Stdout, " %s ", link(authorLink, file.author))
		} else {
			fmt.Fprintf(os.Stdout, " %s ", file.author)
		}

		// If this is a github repo, look for #<issue> links and linkify them.
		// Otherwise just output the first 80 chars of the commit msg. Would it
		// be better to use the full width of the terminal if available here,
		// or just keep it shortish?
		if len(githubUrl) > 0 {
			fmt.Fprintf(os.Stdout, "%s\n", linkify(file.message, githubUrl))
		} else {
			fmt.Fprintf(os.Stdout, "%-80s\n", file.message)
		}
	}
}

func isGithub() string {
	cmd := exec.Command("git", "remote", "-v")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git status: %v", err)
	}

	re := regexp.MustCompile(`https://github.com/[\w-_]+/[\w-_]+`)
	return string(re.Find(out))
}

// gitStatus accepts a dir and a slice of files, and adds the git status to
// each file in place
func gitStatus(files []*File) {
	cmd := exec.Command("git", "status", "--porcelain")
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
			gitStatusMap[fileName] = append(gitStatusMap[fileName], strings.TrimSpace(status))
		}
	}

	for _, file := range files {
		if fileStatus, ok := gitStatusMap[file.entry.Name()]; ok {
			file.status = strings.Join(slices.Compact(fileStatus), ",")
		}
	}
}

func gitLog(files []*File) {
	for _, file := range files {
		cmd := exec.Command("git", "log", "-1", "--date=format:%Y-%m-%d", "--pretty=format:%ad|%aN|%aE|%s", "--", file.entry.Name())
		out, err := cmd.Output()
		if err != nil {
			log.Fatalf("Failed to get git info for file %s: %v", file.entry.Name(), err)
		}

		if len(out) == 0 {
			continue
		}

		parts := strings.SplitN(string(out), "|", 4)
		if len(parts) != 4 {
			log.Fatalf("unexpected output format: %s", out)
		}

		file.lastModified = parts[0]
		file.author = parts[1]
		file.authorEmail = parts[2]
		file.message = parts[3]
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
