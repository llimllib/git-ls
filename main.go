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

type File struct {
	entry        os.DirEntry
	status       string
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
	// Run the 'git status --porcelain' command to get the git status
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
//
// Currently unused: the idea is that I'd like to use it to provide git diff
// status for a directory, by summing up the changes contained within
func first(path string) string {
	i := 0
	for i < len(path) && !os.IsPathSeparator(path[i]) {
		i++
	}
	return path[:i]
}

// git diff output:
// $ git diff --numstat --relative HEAD
// 14      5       main.go
// 0       0       static/fake1
// 0       0       static/fake2
// -       -       static/gitls.png
func gitDiffStat(files []*File) {
	cmd := exec.Command("git", "diff", "--color", "--stat", "--stat-graph-width=4", "--relative", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		log.Fatalf("Diffstat error: %v", err)
	}

	diffStats := make(map[string]string)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "|") {
			parts := strings.Split(line, "|")
			path := first(strings.TrimSpace(parts[0]))
			stats := strings.TrimSpace(parts[1])
			// TODO: aggregate diffs. Right now we're just taking the first
			// diff if a directory has diffs inside it, but we should be
			// summing them up
			diffStats[path] = stats
		}
	}

	for _, file := range files {
		if stats, ok := diffStats[file.entry.Name()]; ok {
			file.diffStat = stats
		}
	}
}
