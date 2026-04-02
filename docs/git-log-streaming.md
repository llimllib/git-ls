# Git Log Streaming Implementation

## Overview

Instead of spawning N git processes in parallel (one per file), we use a single streaming `git log` process that walks history once and finds commit information for all files.

## Performance

**Typical Results:**
- Small repos (10-30 files, <100 commits): **2-3x faster**
- Large repos (500+ files, 80k+ commits): **1.4x faster**, **8-10x less CPU**

**Example (git source repo, 545 files):**
```
Old (parallel):  73.9ms wall,  127.1ms user, 283.9ms system
New (streaming): 25.8ms wall,   18.4ms user,  27.5ms system
Speedup: 2.86x faster
```

## How It Works

### Command
```bash
git log --name-only --date=format:%Y-%m-%d \
  --format='%H\x00%h\x00%ad\x00%aN\x00%aE\x00%s\x00' \
  -n 50000 HEAD -- .
```

**Flags:**
- `--name-only`: List files changed in each commit
- `--format`: Custom format with null-byte separators for easy parsing
- `-n 50000`: Safety limit (don't walk entire history)
- `HEAD -- .`: Start from HEAD, limit to current directory

### Algorithm

```go
func parseGitLogStreaming(files []*File) error {
    // Build map of files we need to find
    filesNeeded := map[string]*File
    
    // Start git log with streaming output
    cmd := exec.Command("git", "log", "--name-only", ...)
    stdout := cmd.StdoutPipe()
    
    // Track stale searches
    commitsSinceLastFind := 0
    
    for each line in stdout {
        if line contains '\x00' {
            // This is commit metadata
            parse and store commit info
            commitsSinceLastFind++
        } else {
            // This is a filename
            firstPart := first(filename)  // "builtin/add.c" -> "builtin"
            
            if firstPart in filesNeeded {
                assign commit info to file
                delete from filesNeeded
                commitsSinceLastFind = 0
                
                if all files found {
                    kill process and exit  // Early exit!
                }
            }
        }
        
        // Give up if stuck
        if commitsSinceLastFind > 5000 {
            kill process and exit
        }
    }
}
```

## Key Design Decisions

### 1. Directory Handling

Git tracks files, not directories. When git log outputs `builtin/add.c`, we extract the first path component (`builtin`) to match against directory names in the current listing.

```go
// Extract first component: "builtin/add.c" -> "builtin"
firstPart := first(filename)
if file, needed := filesNeeded[firstPart]; needed {
    // Match found
}
```

This matches the behavior of `git log -- builtin` which finds commits touching files inside the directory.

### 2. Early Exit Strategies

**Strategy 1: All files found**
```go
if len(filesNeeded) == 0 {
    cmd.Process.Kill()
    return nil
}
```

**Strategy 2: Stuck search**
```go
if commitsSinceLastFind > 5000 {
    cmd.Process.Kill()  // Give up on remaining files
    return nil
}
```

This prevents walking the entire 50k commit limit when files are very old or not in git history.

### 3. Commit Limit

We use `-n 50000` to prevent walking extremely deep history. This is a balance:
- Too low: Miss old files
- Too high: Waste time on missing files
- 50k covers most real-world repositories

### 4. Directory Scope

Using `HEAD -- .` limits to the current directory, which:
- Reduces commits to examine (only those touching current directory)
- Reduces files in output (fewer comparisons)
- Makes early exit happen sooner

## Why It's Faster

### Old Approach (Parallel)
```
for each file:
    spawn git log -1 -- <filename>  # Each walks history independently
    
Wait for all to complete
Slowest file determines wall time
```

**Downsides:**
- N process spawns (2-3ms overhead each)
- N independent history walks (duplicate work)
- Massive system call overhead
- Lock contention on .git directory

### New Approach (Streaming)
```
spawn single git log --name-only
stream output line by line
mark files as found
exit when done
```

**Advantages:**
- Single process (no spawn overhead)
- Single history walk (all files benefit from caching)
- Sequential I/O (better disk cache)
- Minimal system calls
- Early exit (typically finds all files in <1000 commits)

## Trade-offs

**Advantages:**
- 2-3x faster in typical cases
- 8-10x less CPU usage
- Much less system overhead
- Simpler resource model (one git process)

**Disadvantages:**
- More complex code than simple parallel approach
- Files beyond 50k commits won't get info
- If many files are very old and spread out (rare), parallel might win

## Configuration

```go
const HistoryLimit = "50000"       // Max commits to check
const giveUpAfter = 5000           // Exit if no finds in this many commits
```

These can be tuned based on repository characteristics:
- Active repos with recent files: Could reduce both limits
- Archival repos with old files: Might need higher limits

## Fallback

If streaming fails (error reading pipe, etc.), we fall back to the old parallel approach:

```go
if err := parseGitLogStreaming(files); err != nil {
    log.Printf("Warning: git log streaming failed: %v", err)
    parseGitLogParallel(files)  // Fallback
}
```

Deleted files continue to use the parallel approach since they typically require individual `git log` calls (usually only a few files).

## Future Improvements

1. **Adaptive limits**: Start with lower commit limit, increase if needed
2. **Hybrid approach**: Use streaming for first N commits, parallel for remainder
3. **Progress indicator**: Show status for very large repos
4. **Caching**: Store recent commit info between runs
