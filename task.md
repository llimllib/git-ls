# Performance Optimization Tasks for git-ls

## Task 1: Batch Git Log Commands

### Problem

Currently, `parseGitLog()` (lines 373-388) calls `git log -1` **for every file** in the directory. Each call spawns a new subprocess, which involves:

- Process creation overhead
- Shell/git initialization
- Repository index loading
- File system operations

For a directory with N files, this results in N subprocess invocations. In a directory with 50 files, this could add 1-2 seconds of latency.

### Current Implementation

```go
func gitLog(file *File) []byte {
    cmd := exec.Command("git", "log", "-1", "--date=format:%Y-%m-%d",
        "--pretty=format:%h%x00%ad%x00%aN%x00%aE%x00%s", "--", file.entry.Name())
    out, err := cmd.Output()
    // ...
    return out
}

func parseGitLog(files []*File, gitLog func(file *File) []byte) {
    for _, file := range files {
        out := gitLog(file)
        // parse output for each file...
    }
}
```

### Proposed Solution

Replace per-file `git log` calls with a single command that retrieves log info for all files at once.

#### Option A: Use `git log` with `--name-only`

Run a single command to get the most recent commit for each file:

```bash
git log --name-only --date=format:%Y-%m-%d --pretty=format:"%h%x00%ad%x00%aN%x00%aE%x00%s%x00" -- .
```

Then parse the output to build a map of `filename -> log info`, keeping only the most recent entry per file.

**Pros:** Single subprocess call
**Cons:** Returns full history, need to filter to most recent per file; may fetch more data than needed

#### Option B: Use `git log` with multiple path arguments

```bash
git log -1 --date=format:%Y-%m-%d --pretty=format:"%h%x00%ad%x00%aN%x00%aE%x00%s%x00%x01" --name-only -- file1 file2 file3 ...
```

**Pros:** Can limit to recent commits
**Cons:** Command line length limits for large directories; still may not give per-file granularity

#### Option C: Use `xargs` style batching (Recommended)

Batch files into groups and run fewer git commands:

```go
func gitLogBatch(fileNames []string) []byte {
    args := []string{"log", "-1", "--date=format:%Y-%m-%d",
        "--pretty=format:%h%x00%ad%x00%aN%x00%aE%x00%s%x00%x01",
        "--name-only", "--"}
    args = append(args, fileNames...)
    cmd := exec.Command("git", args...)
    // ...
}
```

#### Option D: Use `git whatchanged` or custom format

```bash
git log --format="%h%x00%ad%x00%aN%x00%aE%x00%s%x00%x01" --name-only --date=format:%Y-%m-%d -1 -- *
```

### Recommended Implementation

```go
// New function to get log info for all files in one call
func gitLogAll() []byte {
    cmd := exec.Command("git", "log",
        "--format=%h%x00%ad%x00%aN%x00%aE%x00%s%x00%H",  // %H as record separator
        "--date=format:%Y-%m-%d",
        "--name-only",
        "--")
    out, err := cmd.Output()
    if err != nil {
        log.Fatalf("Failed to get git log: %v", err)
    }
    return out
}

// New parsing function that builds a map
func parseGitLogBatch(files []*File, gitLogOutput []byte) {
    // Build map of filename -> *File for quick lookup
    fileMap := make(map[string]*File, len(files))
    for _, f := range files {
        fileMap[f.entry.Name()] = f
    }

    // Parse the git log output
    // Split by commit records, then for each commit extract:
    // - hash, date, author, email, message
    // - list of files changed
    // For each file, if it's in fileMap and doesn't have log info yet, set it

    // Implementation details:
    // 1. Split output by commit delimiter
    // 2. For each commit, parse metadata and file list
    // 3. For each file in the commit, check if it's in our directory
    // 4. If the file hasn't been assigned log info yet, assign this commit's info
    //    (since git log outputs most recent first, first match wins)
}
```

### Changes Required

1. Remove or deprecate `gitLog(file *File) []byte` function
2. Add new `gitLogAll() []byte` function
3. Rewrite `parseGitLog()` to:
   - Accept the batch output
   - Build a filename -> File map
   - Parse commits and assign to files (first match wins = most recent)
4. Update `main()` to call the new functions

### Expected Impact

- **Before:** N git subprocess calls (where N = number of files)
- **After:** 1 git subprocess call
- **Estimated speedup:** 10-50x for the git log portion, depending on directory size
