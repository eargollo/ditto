# ADR-006: SHA-256 hashing and duplicate definition (symlinks, hardlinks)

**Date**: 2025-02-07

## Decision

1. **Use SHA-256 for content hashing**
   - All content-based duplicate detection uses SHA-256 (Go stdlib `crypto/sha256`). No additional dependencies; hash is stored in the database for grouping and for reuse (e.g. for hardlinks).

2. **Symlinks: skip**
   - Do not follow symlinks; do not hash them. Scan records the path but does not treat the target as part of the tree for hashing. This avoids following links outside the scan root and keeps semantics simple.

3. **Hardlinks: reuse the known hash, no extra hashing**
   - Hardlinks are multiple paths pointing to the same inode (same content by definition). They do not use extra disk space, so the main goal of Ditto (freeing space) does not apply to them; we still report hardlink groups so users can see “these paths are the same file” and optionally remove redundant paths.
   - **Do not hash a path whose inode we have already hashed.** When we encounter a path with an inode that already has a stored hash (from another path in the same scan), assign that existing hash to this path so the file appears in the same duplicate group without reading content again. So: one hash per inode; all paths sharing that inode get the same “known hash” and are reported as one group (e.g. “Hardlinked” or grouped with content duplicates if we use hash as the group key).

## Context

We needed a single, canonical content hash and a clear rule for symlinks and hardlinks. SHA-256 is in the standard library, is collision-resistant, and is a common choice for content identity. Symlinks are skipped to avoid following out-of-tree or recursive links. Hardlinks share blocks so they don’t use extra space; by storing inode (ADR-005) we can detect them and reuse the hash we computed for the first path with that inode, avoiding redundant reads and keeping groups consistent.

## Consequences

- **Positive**
  - No extra dependency for hashing; SHA-256 is well understood and stable.
  - Hardlinks are reported without extra I/O; one hash per inode, reused for all paths with that inode.
  - Symlink handling is simple and safe (no following).
- **Negative**
  - SHA-256 is slower than non-crypto hashes (e.g. xxHash); we rely on the prioritized hash queue and throttling (ADR-005) to manage load. We can revisit a faster hash later if needed.
- **Neutral**
  - Hardlink groups may be shown separately (e.g. “Same file (hardlinks)”) or merged into the same “duplicate group” model with a flag; implementation can choose the best UX.
