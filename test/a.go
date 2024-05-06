package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

func hasTo(e error) {
	if e != nil {
		log.Fatalf("%v", e)
	}
}

func must[T any](a T, e error) T {
	if e != nil {
		log.Fatalf("%v", e)
	}
	return a
}

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

func first(s string) string {
	return strings.Split(s, string(os.PathSeparator))[0]
}

func main() {
	hasTo(os.Chdir(os.Args[1]))

	keys := make(map[string]any)
	files := make(map[string]*File)
	for _, file := range must(os.ReadDir(".")) {
		stat := must(os.Stat(file.Name()))
		keys[file.Name()] = nil
		files[file.Name()] = &File{
			entry: file,
			isDir: file.IsDir(),
			isExe: !file.IsDir() && stat.Mode()&0111 != 0,
		}
	}

	r := must(git.PlainOpen("."))
	ref := must(r.Head())

	// ... retrieves the commit history
	// Possibly using PathFilter would work? But I'm not sure if I can break
	// out of the loop once I've found the most recent commits for all my
	// files?
	cIter := must(r.Log(&git.LogOptions{From: ref.Hash()}))

	// ... just iterates over the commits, printing it
	hasTo(cIter.ForEach(func(c *object.Commit) error {
		// let's try to check the files in the commit
		// Nope, this lists _every_ file in the tree at the time
		// of the commit.
		// must(c.Files()).ForEach(func(f *object.File) error {
		// 	fmt.Println(f.Name)
		// 	return nil
		// })
		for _, fstat := range must(c.Stats()) {
			if _, ok := keys[string(fstat.Name)]; ok {
				fmt.Printf("+%d -%d %s %s %s\n", fstat.Addition, fstat.Deletion, fstat.Name, c.Hash, strings.Split(c.Message, "\n")[0])
				delete(keys, fstat.Name)
			} else {
				// fmt.Printf("%s not in %#v\n", fstat.Name, keys)
			}
		}
		if len(keys) == 0 {
			return storer.ErrStop
		}
		return nil
	}))
}
