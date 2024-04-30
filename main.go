package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
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
		files = append(files, &File{entry: file})
	}

	gitStatus(files)
	gitLog(files)
	show(files)
}

func show(files []*File) {
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
	fmtString := "%-" + strconv.Itoa(maxNameLen) + "s %s %-" + strconv.Itoa(maxAuthorLen) + "s %-50s\n"
	for _, file := range files {
		fmt.Printf(fmtString, file.entry.Name(), file.lastModified, file.author, file.message)
	}
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
