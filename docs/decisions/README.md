# Architecture Decision Records

This folder contains **Architecture Decision Records (ADRs)** for the Ditto project. Each ADR captures an important architectural or technical decision, its context, and consequences.

## Format

- **Header**: ADR title and **Date** (e.g. `**Date**: YYYY-MM-DD`)
- **Decision**: What we decided to do
- **Context**: What situation or problem led to this decision
- **Consequences**: Benefits, drawbacks, and follow-ups

## Index

| ADR       | Title                                                    |
|-----------|----------------------------------------------------------|
| ADR-001   | Packaging strategy for Synology deployment                |
| ADR-002   | SQLite for persistent storage with external data mount   |
| ADR-003   | Web UI as primary user experience                        |
| ADR-004   | HTMX and Tailwind for the Web UI                         |
| ADR-005   | Separate scan from hashing with prioritized hash queue   |
| ADR-006   | SHA-256 hashing and duplicate definition (symlinks, hardlinks) |
