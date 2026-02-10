# ADR-008: Scan hangs on FUSE/cloud paths and default exclude file

**Date**: 2026-02-08

## Decision

1. **Do not rely on in-process timeouts for directory listing**
   - `os.ReadDir` (and similar syscalls) can block indefinitely on some paths (FUSE mounts, cloud storage, macOS protected dirs). Go’s in-process timeout pattern (goroutine + `select` with `time.After`) does **not** reliably fire when the blocking call runs in the same process, because the scheduler cannot preempt a thread stuck in a syscall. We do not implement ReadDir/Lstat/callback timeouts.

2. **Use a default exclude file that the application always applies**
   - Ship an embedded **default ignore file** (`internal/scan/default.dittoignore`) that is always loaded and merged with any root-level `.dittoignore`. This gives us a single place to add path patterns that are known to hang or are not useful to scan (e.g. cloud sync “.Encrypted” dirs), without requiring users to configure anything.

3. **Keep permission-error handling**
   - When the walk encounters a permission/access error (e.g. “operation not permitted” on macOS `Library/Calendars`), we log and return `filepath.SkipDir` so the walk continues. This is unchanged and handles errors that the kernel returns; it does not help when the kernel/syscall never returns.

## Context

During full-tree scans (e.g. home directory including `Library`), the scan sometimes hung indefinitely. Debugging showed the hang occurred in `os.ReadDir` on paths such as:

- `.../Library/CloudStorage/GoogleDrive-.../.Encrypted/.shortcut-targets-by-id/...`
- Other FUSE or cloud-storage-backed directories

These paths can block inside the kernel or file provider; the call never returns and no error is reported. We attempted to wrap `ReadDir` (and Lstat, and the insert callback) in a goroutine with a 10s timeout using `select` and `time.After`. The timeout **did not fire** even after several minutes, so the scan remained stuck.

**Why in-process timeouts fail**

- The Go scheduler is **semi-preemptive**. It does not preempt a goroutine that is blocked in **external code** (e.g. a syscall or C library). So when a goroutine is stuck in `os.ReadDir` on a path that never returns, that OS thread is blocked. The goroutine that is waiting in `select` on `time.After(10*time.Second)` may never get scheduled if all runnable threads are occupied or if the runtime does not run the timer on another thread. In practice, timers do not provide a guarantee when the process is blocked in a syscall.

- **References**
  - [How do I timeout a blocking external library call?](https://stackoverflow.com/questions/48604783/how-do-i-timeout-a-blocking-external-library-call) — Same pattern (goroutine + `select` + `time.After`); author reports the timeout case never runs and the call blocks for the full 60 seconds. Accepted answer explains that the scheduler cannot preempt while in external code; `runtime.LockOSThread()` in the worker goroutine did not fix it.
  - [Golang cmd.Start() hanging on fuse mounted dir sometimes](https://stackoverflow.com/questions/54203525/golang-cmd-start-hanging-on-fuse-mounted-dir-sometimes) — `cmd.Start()` (which does a `Stat()` on the working dir) hangs when the dir is a FUSE mount; solution suggested is to use a child process with a timeout (e.g. `CommandContext`), i.e. process isolation rather than in-process timeout.

**Alternatives considered**

- **Subprocess per directory (or per blocking op)**  
  Run `ReadDir` in a child process; parent waits with a timeout and kills the child if it does not exit. This is the only way to get a **guaranteed** timeout when the kernel/driver blocks. Rejected for now: higher complexity (helper binary or mode, serializing directory results), and we prefer a simpler, “skip known-bad paths” approach first.

- **Built-in hardcoded list of path segments to skip**  
  Same idea as the default ignore file but expressed in code (e.g. a slice of strings). We chose an **embedded file** so that adding or changing patterns does not require code changes and matches the existing `.dittoignore` format.

- **Rely only on user-configured excludes**  
  We could document that users should add patterns (e.g. `Library/CloudStorage`, `.Encrypted`) to their `.dittoignore`. That would not be robust: we cannot assume users will do this, and new problematic paths (e.g. other cloud providers) would still cause hangs. The default file ensures we always skip at least the paths we know are problematic.

## Consequences

- **Positive**
  - Scans no longer hang on the paths we add to the default ignore file (e.g. `.Encrypted` under cloud storage). No timeout machinery to maintain; behaviour is easy to reason about.
  - One place to extend: edit `default.dittoignore` (and optionally document in the ADR or release notes) when we learn of new path patterns that hang or are not useful to scan.
  - Same pattern format as root `.dittoignore`; root file is merged so user overrides and extra patterns still apply.
- **Negative**
  - We do not scan inside excluded paths at all. If a user needs to scan inside e.g. `.Encrypted`, they cannot do so without changing the application (or we would need a way to override defaults, not currently provided).
  - New hang-prone paths (e.g. other FUSE mounts or cloud dirs) will still hang until we add a pattern or the user adds one to their `.dittoignore`. The default file improves robustness but does not guarantee no hang for arbitrary paths.
- **Neutral**
  - If we later need a guaranteed timeout (e.g. for arbitrary roots), we can adopt the subprocess approach (ADR or follow-up) and keep the default exclude file as a first line of defence.
