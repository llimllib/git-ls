# git-ls

list the files in the current directory along with a useful summary of their git status.

Example:

```
$ git ls
          .git
          .gitignore 2024-04-30  Bill Mill add gitignore
          LICENSE    2024-05-01  Bill Mill chore: add unlicense
          NOTES
          git-ls
          go.mod     2024-04-30  Bill Mill add go.mod
 M 6 ++-- main.go    2024-05-01  Bill Mill feat: show diffstat
```

The output is nicely colored:

![](static/gitls.png)

## building

Run `go build`

## installing

Put `git-ls` anywhere on your path, and you can then call it with `git ls`
