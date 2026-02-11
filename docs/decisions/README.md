# Architecture Decision Records

This folder contains **Architecture Decision Records (ADRs)** for the Ditto project. Each ADR captures an important architectural or technical decision, its context, and consequences.

## Format

- **Header**: ADR title and **Date** (e.g. `**Date**: YYYY-MM-DD`)
- **Decision**: What we decided to do
- **Context**: What situation or problem led to this decision
- **Consequences**: Benefits, drawbacks, and follow-ups

## Index

| ADR       | Title                                                    | Status |
|-----------|----------------------------------------------------------|--------|
| ADR-001   | Packaging strategy for Synology deployment                | Active |
| ADR-002   | SQLite for persistent storage with external data mount   | **Superseded** by Release 0.2 (PostgreSQL) |
| ADR-003   | Web UI as primary user experience                        | Active |
| ADR-004   | HTMX and Tailwind for the Web UI                         | Active |
| ADR-005   | Separate scan from hashing with prioritized hash queue   | Active |
| ADR-006   | SHA-256 hashing and duplicate definition (symlinks, hardlinks) | Active |
| ADR-007   | Absolute paths and scan as source of freshness and deletion     | **Partially superseded** by Release 0.2 (ledger-based model) |
| ADR-008   | Scan hangs on FUSE/cloud paths and default exclude file         | Active |
| ADR-009   | PostgreSQL and new data model (Release 0.2)                      | Active |