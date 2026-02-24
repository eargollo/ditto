# ADR-010: File deletion from duplicate groups (safety rules)

**Date**: 2026-02-23

## Unacceptable outcomes (we must never do these)

Deletion is **irreversible**. We must never:

1. **Delete a file that has been changed** — content on disk no longer matches what we stored when the group was formed.
2. **Delete the wrong file** — e.g. due to client bug, UI mix-up, or malicious request.
3. **Delete a unique file** — the only remaining copy in the group (user would lose the only instance).

## Decision

1. **Deletion is permanent and scoped to hash groups**
   - Users can delete a file from a duplicate group via a per-file trash icon on the home page. Deletion means: remove the file from disk (`os.Remove`) and set `deleted_at` in the DB. Only duplicate-by-hash (content) groups are supported; inode (hardlink) groups are out of scope.

2. **Client sends only file_id**
   - The delete API accepts only `file_id` (integer). All paths and hashes used for hashing or deleting **must** be loaded from the database by that ID. We never use client-supplied path or hash for any I/O. This prevents deleting the wrong file (no path confusion or injection).

3. **At least one file must remain in the group**
   - Before any delete, we count non-deleted files in the group (same hash, `deleted_at IS NULL`) that **exist on disk and have unchanged size and mtime**. If that validated count is ≤ 1, we reject. We never delete the only copy.

4. **Re-hash verification before delete**
   - We re-hash the file to delete (path from DB for that file_id) and one other file in the group (path from DB). Both computed hashes must equal the **stored** hashes for those files. If either differs, we reject. This prevents deleting a changed file or acting on stale group state.

## Context

Ditto helps users reclaim space by identifying duplicate files. Letting users delete one copy from the UI requires strict safeguards because deletion is irreversible. The three unacceptable outcomes above are prevented by: (1) only accepting file_id and deriving all paths/hashes from DB, (2) validating remaining files on disk (existence + size/mtime) and rejecting when ≤ 1 remain, and (3) re-hashing both the file to delete and one other file and comparing to stored hashes before any disk delete.

## Consequences

- **Positive**
  - Deletion is guarded against the three unacceptable outcomes: we never delete a unique file, a changed file, or the wrong file. All I/O uses DB-derived paths and hashes; the client cannot supply paths.
  - Stale or changed files are not deleted; user can refresh the group and retry.
- **Negative**
  - Two full-file reads (to delete + one other) per delete; acceptable for interactive use.
- **Neutral**
  - ADR can be updated as we add e.g. "move to trash" vs permanent delete, or inode-group deletion.
