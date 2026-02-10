# Release 2, Phase 2: Progress and feedback (improvements)

**Goal:** Give users visibility into long-running scan and hash operations. The UI should show what is happening under the hood: progress bars where meaningful, and detail such as the file currently being hashed, so that a long run does not feel stuck.

**Context:** After Release 1, starting a scan redirects to a progress page that polls status. Today we only show high-level timestamps and final counts (e.g. hashed file count after hash phase completes). During the run there is no indication of progress or current activity, which can make the experience feel slow or unresponsive.

**References:** [ADR-004](../decisions/ADR-004-htmx-tailwind.md) (HTMX, Tailwind). Builds on Release 1 progress page and status fragment.

---

## TDD and review

- Progress data and API: test that the status (or a dedicated progress) endpoint returns the new fields when available.
- UI: manual verification or light integration test that the progress page shows a bar and current file when a scan/hash is running.

---

## Step 1: Progress data for scan phase

**What:** Expose “scan phase” progress so the UI can show that the scan is advancing (e.g. number of files discovered so far). No total count until the scan completes.

**Options:**
- **A)** Use existing data: while the scan is running, the walk is inserting rows; the status endpoint can report `files_discovered_so_far = COUNT(*) FROM files WHERE scan_id = ?`. Polling the existing status endpoint already gives an increasing count until `completed_at` is set.
- **B)** Add a lightweight progress table or column (e.g. `scans.files_walked_count` updated periodically by the walk). Only needed if we want to avoid a COUNT query or to show “directories walked” separately.

**Deliverables:**
- Status (or progress) response includes a field for “files discovered so far” during scan phase (e.g. from COUNT or from a new column). When scan is complete, “total files” is available.
- Progress page or fragment can display: “Scanning… X files found” (and when done: “Scan complete. X files.”).

**Review:** While a scan is running, the user sees the file count increase over time.

---

## Step 2: Progress data for hash phase

**What:** Expose hash-phase progress: how many files (or bytes) have been hashed so far, and optionally which file is currently being hashed. This requires the hash pipeline to update progress somewhere that the HTTP layer can read.

**Options:**
- **A)** Update scan row periodically: in `hash.RunHashPhase` (or workers), every N files (e.g. 10 or 50), call `db.UpdateScanHashProgress(scanID, hashedCount, hashedBytes)`. Add columns like `hashed_file_count` and `hashed_byte_count` (we may already have these; they are currently set only at completion). If we already have them, change semantics to “current count/bytes” and set them periodically during the run, then set again at completion.
- **B)** “Current file”: store the path of the file currently being hashed (e.g. in-memory map `scanID -> currentPath` in the server, updated by the goroutine that runs the hash phase, or a small `scan_progress` table). The status/progress endpoint returns this so the UI can show “Hashing: /path/to/file”.

**Deliverables:**
- Hash phase writes progress at least periodically (e.g. `hashed_file_count`, `hashed_byte_count` on the scan row, or equivalent). Total to hash = file count from scan phase once scan is complete.
- Optional but recommended: “current file path” exposed via status/progress API when hash is running.
- Progress fragment/API returns: `hash_progress` (e.g. hashed count and total count, and optionally hashed bytes), and `current_file` when available.

**Review:** While the hash phase is running, the user sees hashed count (and optionally bytes and current file) updating.

---

## Step 3: Progress bar and current-activity in the UI

**What:** Use the progress data to render a progress bar and a line of detail (e.g. “Hashing: /path/to/current/file”) on the scan progress page.

**Deliverables:**
- Progress page (or its HTMX-loaded status fragment) shows:
  - **Scan phase:** “Scanning… X files found” (no bar, or an indeterminate indicator). When done: “Scan complete. X files.”
  - **Hash phase:** A progress bar (e.g. “Hashed M of N files” and optionally “X MB of Y MB”), and a line such as “Currently hashing: /path/to/file” when `current_file` is present.
- Use Tailwind (or existing CSS) for the bar. Keep the existing polling (e.g. every 2s); the fragment just includes the new markup and values.

**Review:** User sees a clear notion of progress and knows what is happening (e.g. which file is being hashed) during long runs.

---

## Step 4: Show duplicates as they become available

**What:** Allow users to view duplicates even before the scan (or hash phase) has finished. Today the “View duplicates” link appears only when both scan and hash are complete. We can show partial results earlier so the user gets value during long runs.

**Data we can show early:**
- **By inode (hardlinks):** As soon as the scan has inserted file rows, we have `inode` and `device_id`. Inode duplicate groups can be computed and shown as soon as the scan phase has completed (or even during scan, with the usual caveat that counts may still increase).
- **By size (candidate duplicates):** Same-size groups are known after the scan phase; we can list “candidates by size” (groups of files with the same size) before any hashing. These are not content-verified duplicates but useful as a preview.
- **By hash (content duplicates):** Once some files have been hashed, we can show hash-based duplicate groups for the hashed subset. The list grows as more files get `hash_status = 'done'`. Clearly label that results are partial while hash is still running (e.g. “Duplicate groups from X hashed files so far; hashing in progress.”).

**Deliverables:**
- The “View duplicates” entry point (e.g. link or button) is available on the progress page as soon as there is something to show: e.g. after scan phase completes (for inode + size candidates), or as soon as at least one duplicate group exists (by size or by hash).
- Duplicates page GET `/scans/{id}/duplicates` is reachable and returns useful data even when the scan is not complete or the hash phase is still running. Show inode groups when file data exists; show size-based “candidate” groups (same size, count > 1) with a clear label; show hash-based groups for files that are already hashed, with a note when hash is in progress (e.g. “Partial results; hashing in progress.”).
- Optional: a dedicated “candidates by size” section (same-size groups) that is available right after scan phase, with a link to “content duplicates” (by hash) that gains more groups as hashing progresses.

**Review:** User can open the duplicates view before the run has finished and see inode duplicates, size-based candidates, and/or partial hash-based duplicates, with clear labeling when results are still updating.

---

## Release 2, Phase 2 done when

- [ ] Scan phase shows “files discovered so far” (and “Scan complete. X files” when done).
- [ ] Hash phase shows progress (e.g. hashed count and total, and optionally bytes).
- [ ] Optionally: “current file” is shown during hash (e.g. “Hashing: /path/to/file”).
- [ ] Progress bar is visible for the hash phase (e.g. M of N files).
- [ ] All of the above work with the existing polling mechanism; no regression to Release 1 behavior.
- [ ] Duplicates can be viewed before the scan/hash has finished: inode groups and size-based candidates when data exists; hash-based groups (partial) while hashing, with clear “partial / in progress” labeling.
