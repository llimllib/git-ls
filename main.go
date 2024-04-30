package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type File struct {
	entry        os.DirEntry
	status       string
	author       string
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

func show(dir string, files []*File, githubUrl string) {
	maxNameLen := 0
	maxAuthorLen := 0
	for _, file := range files {
		if len(file.entry.Name()) > maxNameLen {
			maxNameLen = len(file.entry.Name())
		}
		if len(file.author) > maxAuthorLen {
			maxAuthorLen = len(file.author)
		}
	}
	// We have to calculate the file name's format separately, because it
	// contains a big escaped hyperlink that printf won't format properly
	fileNameFmt := "%-" + strconv.Itoa(maxNameLen) + "s"
	fmtString := " %s %-" + strconv.Itoa(maxAuthorLen) + "s "
	for _, file := range files {
		if file.isDir {
			os.Stdout.WriteString(BLUE)
		}
		if file.isExe {
			os.Stdout.WriteString(GREEN)
		}
		os.Stdout.WriteString(link(
			"file:"+dir+"/"+file.entry.Name(),
			fmt.Sprintf(fileNameFmt, file.entry.Name())))
		if file.isDir || file.isExe {
			os.Stdout.WriteString(RESET)
		}
		fmt.Printf(fmtString, file.lastModified, file.author)

		// If this is a github repo, look for #<issue> links and linkify them.
		// Otherwise just output the first 80 chars of the commit msg. Would it
		// be better to use the full width of the terminal if available here,
		// or just keep it shortish?
		if len(githubUrl) > 0 {
			os.Stdout.WriteString(linkify(file.message, githubUrl))
			os.Stdout.WriteString("\n")
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

	re := regexp.MustCompile(`https://github.com/\w+/\w+`)
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

	gitStatusMap := make(map[string]string)
	lines := strings.Split(string(out), "\n")

	for _, line := range lines {
		if len(line) >= 3 {
			status := line[:2]
			fileName := line[3:]
			gitStatusMap[fileName] = status
		}
	}

	for _, file := range files {
		if fileStatus, ok := gitStatusMap[file.entry.Name()]; ok {
			file.status = fileStatus
		}
	}
}

func gitLog(files []*File) {
	for _, file := range files {
		cmd := exec.Command("git", "log", "-1", "--date=format:%Y-%m-%d", "--pretty=format:%ad|%an|%s", "--", file.entry.Name())
		out, err := cmd.Output()
		if err != nil {
			log.Fatalf("Failed to get git info for file %s: %v", file.entry.Name(), err)
		}

		if len(out) == 0 {
			continue
		}

		parts := strings.SplitN(string(out), "|", 3)
		if len(parts) != 3 {
			log.Fatalf("unexpected output format: %s", out)
		}

		file.lastModified = parts[0]
		file.author = parts[1]
		file.message = parts[2]
	}
}
