# Phase 5a: Weekly scheduled scan

**Goal:** Run a full scan of all configured folders automatically on a weekly schedule. The user chooses which weekday and hour the job runs. Only one scan runs at a time (existing serialized queue); manual "Start scan" / "Continue" still enqueue jobs as today.

**Context:** After Phase 5 we have scan roots, a serialized scan queue, and manual triggers. Phase 5a adds an in-process scheduler: wake every hour (at the top of the hour), and when the current weekday and hour match the configured schedule, enqueue one scan per folder so they run back-to-back.

**References:** Existing scan queue and worker in server; `scan_roots` (or equivalent) for the list of folders.

---

## Design choices (agreed)

- **Granularity:** Hourly only. User picks weekday (e.g. Sunday) and hour (0–23). No minutes.
- **Wake interval:** Scheduler goroutine wakes at the **top of each hour** (e.g. 00:00, 01:00, 02:00). On startup, first wake is the next round hour to avoid drift.
- **Trigger logic:** On each wake, compare current weekday and hour to the stored schedule. If they match, enqueue one scan per folder (create scan row per root, then send each scan ID to the existing `scanQueue`). No "next run at" stored; "run when weekday + hour match" is enough.
- **One job at a time:** Unchanged. The existing worker drains the queue; weekly run just adds N jobs (one per folder) to that queue.
- **Manual runs:** User can still click "Start scan" or "Continue" anytime; those jobs are enqueued as today. No change to that flow.

---

## TDD and review

- Scheduler: unit or integration test that with a fixed clock (or time injection), when weekday and hour match config, the enqueue logic runs (e.g. N scan IDs sent to queue or N scans created). When they don't match, nothing is enqueued.
- Config: test that schedule (weekday, hour) is read from DB or config and used correctly.
- Manual check: set schedule to "current weekday, next hour", wait for wake (or mock time), confirm scans are created and queued.

---

## Step 1: Schedule config storage

**What:** Persist the user's choice of weekday and hour for the weekly run.

**Options:**
- **A)** Single row in a small table, e.g. `schedule_config (weekday INTEGER, hour INTEGER)`. Only one schedule for the whole app (all folders run at the same time).
- **B)** Column on an existing table if we have a "settings" or "app_config" table.
- **C)** Env or config file. Less flexible; DB is consistent with scan roots.

**Deliverables:**
- Migration: table or columns for `schedule_weekday` (0–6 or 1–7 per Go `time.Weekday`) and `schedule_hour` (0–23). Nullable or default "disabled" (e.g. null = no automatic run).
- DB helpers: get schedule, update schedule (used by UI in a later step).

**Review:** Schedule can be read and written; default is "no schedule" (skip weekly run) until user sets one.

---

## Step 2: Scheduler goroutine

**What:** Run a loop that wakes at the top of each hour and, if the schedule is set and current weekday + hour match, enqueue one scan per folder.

**Details:**
- Start the goroutine from the same place the scan worker is started (e.g. `Run(ctx)`). Use the same `ctx` so shutdown cancels it.
- Sleep until next round hour: `now.Truncate(time.Hour).Add(time.Hour)` (or equivalent) so first wake is at :00. Then `time.Tick(1 * time.Hour)` or sleep 1h in a loop (with `select` on `ctx.Done()` so we don't block shutdown).
- On wake: get schedule from DB. If not set (null), skip. If set, get current weekday and hour (use local time or configurable TZ; default local). If they match, load all scan roots, for each root create a new scan row (`CreateScan`), then send each scan ID to `s.scanQueue` (non-blocking; if queue full, log and skip remaining or retry one).
- Use the **write** DB for creating scans and enqueueing; schedule read can use read DB if desired.

**Deliverables:**
- Scheduler goroutine that wakes every hour at :00.
- Match logic: weekday + hour.
- Enqueue path: list roots → for each, create scan → enqueue scan ID. No duplicate "in progress" check required for the scheduled run (each run is a new scan row).

**Review:** When weekday and hour match, N scans are created and N IDs are sent to the queue. When they don't match, no scans created.

---

## Step 3: UI for schedule

**What:** Let the user set (and see) the weekly schedule.

**Details:**
- Page or section (e.g. on scans page or a small "Settings" / "Schedule" section): dropdown or select for weekday (Monday–Sunday), dropdown for hour (0–23 or 12h AM/PM). Save button writes to DB (update schedule config).
- Show current schedule: "Weekly scan: Sunday at 02:00" (or "Not set").
- Optional: "Next run: …" (next matching weekday+hour); can be computed from current time and schedule.

**Deliverables:**
- Form to set weekday and hour; POST updates DB.
- Display current schedule on the same page or scans page.

**Review:** User can set and see the weekly run time; after setting, the scheduler uses it on the next hourly wake.

---

## Out of scope (for later)

- Per-folder schedule (e.g. folder A on Sunday, folder B on Monday).
- "Run now" button for the scheduled job (user can already "Start scan" per folder).
- "If we missed the hour" run on startup (can add in an improvement).
- Timezone selection (use server local time for 5a).
