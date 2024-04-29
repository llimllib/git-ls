package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

func main() {
	// Check if a directory argument is provided
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <directory>", os.Args[0])
	}

	dir := os.Args[1]

	// Get the current working directory
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get the current working directory: %v", err)
	}

	// Change the current working directory to the provided directory
	if err := os.Chdir(dir); err != nil {
		log.Fatalf("Failed to change directory to %s: %v", dir, err)
	}
	defer os.Chdir(cwd) // Change back to the original working directory when done

	// Run the 'git status --porcelain' command to get the git status
	cmd := exec.Command("git", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get git status: %v", err)
	}

	// Parse the git status output
	gitStatusMap := parseGitStatus(string(out))

	// List files in the directory and annotate with git status
	files, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("Failed to read directory %s: %v", dir, err)
	}

	for _, file := range files {
		fileName := file.Name()
		fileStatus, ok := gitStatusMap[fileName]
		if ok {
			fmt.Printf("%s %s\n", fileStatus, fileName)
		} else {
			fmt.Printf("   %s\n", fileName)
		}
	}
}

func parseGitStatus(gitStatusOutput string) map[string]string {
	gitStatusMap := make(map[string]string)
	lines := strings.Split(gitStatusOutput, "\n")

	for _, line := range lines {
		if len(line) >= 3 {
			status := line[:2]
			fileName := line[3:]
			gitStatusMap[fileName] = status
		}
	}

	return gitStatusMap
}
