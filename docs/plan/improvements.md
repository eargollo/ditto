# Improvements (backlog)

Items we may do after the main phases. Not in phase order.

## Scan

- **Concurrent directory reads** – Run `os.ReadDir` for multiple directories in parallel (e.g. a bounded worker pool that lists dirs and feeds paths to the walk). May improve scan throughput on large trees; reuse the same timeout-per-dir behaviour so slow/hanging dirs don’t block the rest.
