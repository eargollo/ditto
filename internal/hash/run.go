package hash

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eargollo/ditto/internal/db"
	"golang.org/x/time/rate"
)

const hashProgressLogInterval = 50   // log "N/M files" every this many files
const slowOpThreshold = 100 * time.Millisecond // log when a single DB op exceeds this (for investigation)
const hashJobChannelCap = 1000       // bounded channel for producer-consumer; backpressure if consumers are slow
const fileLogInterval = 5 * time.Second // at most one per-file log line every this long (avoid flooding)

func logSlowIf(op string, start time.Time) {
	if d := time.Since(start); d > slowOpThreshold {
		log.Printf("[hash] slow: %s took %v", op, d)
	}
}

var (
	fileLogMu       sync.Mutex
	fileLogLastTime time.Time
)

// logFileIfThrottled logs the message at most once per fileLogInterval (globally across workers).
func logFileIfThrottled(format string, args ...interface{}) {
	fileLogMu.Lock()
	defer fileLogMu.Unlock()
	if time.Since(fileLogLastTime) < fileLogInterval {
		return
	}
	fileLogLastTime = time.Now()
	log.Printf(format, args...)
}

// HashOptions configures the hash phase. Nil means defaults (single worker, no throttle).
type HashOptions struct {
	Workers             int // number of workers (default 1)
	MaxHashesPerSecond  int // 0 = no throttle
}

func (o *HashOptions) workers() int {
	if o == nil || o.Workers <= 0 {
		return 1
	}
	return o.Workers
}

func (o *HashOptions) maxHashesPerSecond() int {
	if o == nil {
		return 0
	}
	return o.MaxHashesPerSecond
}

// RunHashPhase runs the hash phase for the given scan: resets any orphaned 'hashing' to 'pending',
// sets hash_started_at, then runs a producer-consumer pipeline (one query streams pending jobs to a channel,
// N workers process them). Sets hash_completed_at when done. Respects context cancellation.
func RunHashPhase(ctx context.Context, database *sql.DB, scanID int64, opts *HashOptions) error {
	if err := db.ResetHashStatusHashingToPending(ctx, database, scanID); err != nil {
		return err
	}
	if err := db.UpdateScanHashStartedAt(ctx, database, scanID); err != nil {
		return err
	}
	db.ResetBusyRetryCount()
	total, _ := db.CountHashCandidates(ctx, database, scanID) // best-effort for progress; 0 on error
	n := opts.workers()
	log.Printf("[hash] phase started for scan %d (%d worker(s), %d files to hash)", scanID, n, total)
	phaseStart := time.Now().UTC()
	var completed, reusedCount, hashErrorCount atomic.Int64
	err := runHashPhaseProducerConsumer(ctx, database, scanID, total, &completed, &reusedCount, &hashErrorCount, phaseStart, opts, n)
	if err != nil {
		log.Printf("[hash] phase failed for scan %d: %v", scanID, err)
		return err
	}
	fileCount, byteCount, err := db.GetHashedFileCountAndBytes(ctx, database, scanID)
	if err != nil {
		return err
	}
	log.Printf("[hash] phase completed for scan %d: %d files, %d bytes, %d reused, %d errors (SQLITE_BUSY retries: %d)", scanID, fileCount, byteCount, reusedCount.Load(), hashErrorCount.Load(), db.BusyRetryCount())
	return db.UpdateScanHashCompletedAt(ctx, database, scanID, fileCount, byteCount, reusedCount.Load(), hashErrorCount.Load())
}

// runHashPhaseProducerConsumer: one producer sends pending jobs (from a single SELECT) to a bounded channel;
// N consumers process jobs and update the DB. Producer closes channel when done; consumers exit when channel is closed.
func runHashPhaseProducerConsumer(ctx context.Context, database *sql.DB, scanID int64, total int64, completed, reusedCount, hashErrorCount *atomic.Int64, phaseStart time.Time, opts *HashOptions, numWorkers int) error {
	jobs := make(chan *db.File, hashJobChannelCap)
	errCh := make(chan error, 1) // first error from producer or any consumer

	// Producer: stream pending jobs from one query into the channel; close when done or on error.
	go func() {
		defer close(jobs)
		err := db.ForEachPendingHashJob(ctx, database, scanID, func(f *db.File) error {
			select {
			case jobs <- f:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if err != nil && err != context.Canceled {
			select {
			case errCh <- err:
			default:
			}
		}
	}()

	// Consumers: read from channel until closed; process each job.
	var wg sync.WaitGroup
	now := time.Now().UTC()
	var limiter *rate.Limiter
	if opts != nil && opts.MaxHashesPerSecond > 0 {
		limiter = rate.NewLimiter(rate.Limit(opts.MaxHashesPerSecond), 1)
	}
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				reused, err := processClaimedJob(ctx, database, job, opts, now, limiter)
				if err != nil {
					if hashErrorCount != nil {
						hashErrorCount.Add(1)
					}
					_ = db.ResetFileHashStatusToPending(ctx, database, job.ID) // return to queue so it can be retried
					select {
					case errCh <- err:
					default:
					}
					return
				}
				if reused && reusedCount != nil {
					reusedCount.Add(1)
				}
				progressLog(completed, total, phaseStart)
			}
		}()
	}

	wg.Wait()
	select {
	case runErr := <-errCh:
		return runErr
	default:
		return nil
	}
}

// processClaimedJob hashes the file (or reuses inode/previous hash). Returns (reused, nil) on success, (false, err) on error.
func processClaimedJob(ctx context.Context, database *sql.DB, job *db.File, opts *HashOptions, now time.Time, limiter *rate.Limiter) (reused bool, err error) {
	// Same-scan inode reuse (hardlink)
	t0 := time.Now()
	h, err := db.HashForInode(ctx, database, job.ScanID, job.Inode, job.DeviceID)
	logSlowIf("HashForInode", t0)
	if err != nil {
		return false, err
	}
	if h != "" {
		logFileIfThrottled("[hash] reused (inode) %s [%s]", job.Path, filepath.Base(job.Path))
		t1 := time.Now()
		err := db.UpdateFileHash(ctx, database, job.ID, h, now)
		logSlowIf("UpdateFileHash", t1)
		return true, err
	}
	// Previous-scan unchanged file reuse
	t2 := time.Now()
	h, err = db.HashForInodeFromPreviousScan(ctx, database, job.ScanID, job.Inode, job.DeviceID, job.Size)
	logSlowIf("HashForInodeFromPreviousScan", t2)
	if err != nil {
		return false, err
	}
	if h != "" {
		logFileIfThrottled("[hash] reused (unchanged) %s [%s]", job.Path, filepath.Base(job.Path))
		t3 := time.Now()
		err := db.UpdateFileHash(ctx, database, job.ID, h, now)
		logSlowIf("UpdateFileHash", t3)
		return true, err
	}
	// Throttle before reading (Step 6)
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return false, err
		}
	}
	logFileIfThrottled("[hash] hashing %s [%s] (%d bytes)", job.Path, filepath.Base(job.Path), job.Size)
	h, err = HashFile(job.Path)
	if err != nil {
		logFileIfThrottled("[hash] failed %s [%s]: %v", job.Path, filepath.Base(job.Path), err)
		return false, err
	}
	logFileIfThrottled("[hash] hashed %s [%s]", job.Path, filepath.Base(job.Path))
	t4 := time.Now()
	err = db.UpdateFileHash(ctx, database, job.ID, h, now)
	logSlowIf("UpdateFileHash", t4)
	return false, err
}

// progressLog logs "N/M files (X%)" and optionally ETA every hashProgressLogInterval or when done.
// Rate = n/elapsed from start; remaining = (total-n)/rate; ETA = now+remaining. All values kept non-negative.
func progressLog(completed *atomic.Int64, total int64, phaseStart time.Time) {
	if total <= 0 {
		return
	}
	n := completed.Add(1)
	if n%hashProgressLogInterval != 0 && n != total {
		return
	}
	// Cap at total so we never show >100% or negative remaining when n races past total.
	displayN := n
	if displayN > total {
		displayN = total
	}
	pct := float64(100) * float64(displayN) / float64(total)
	msg := fmt.Sprintf("[hash] progress: %d/%d files (%.1f%%)", displayN, total, pct)
	elapsed := time.Since(phaseStart)
	if displayN >= total {
		msg += fmt.Sprintf(" | done in %s", formatDuration(elapsed))
		log.Print(msg)
		return
	}
	// Mid-run: rate from start, extrapolate remaining and ETA (all positive).
	if displayN <= 0 || elapsed <= time.Second {
		log.Print(msg)
		return
	}
	elapsedSec := elapsed.Seconds()
	if elapsedSec <= 0 {
		log.Print(msg)
		return
	}
	rate := float64(displayN) / elapsedSec
	if rate <= 0 {
		log.Print(msg)
		return
	}
	remainingSec := float64(total-displayN) / rate
	if remainingSec < 0 {
		remainingSec = 0
	}
	remaining := time.Duration(remainingSec * float64(time.Second))
	if remaining < 0 {
		remaining = 0
	}
	eta := time.Now().Add(remaining)
	msg += fmt.Sprintf(" | elapsed %s | remaining ~%s | ETA ~%s",
		formatDuration(elapsed), formatDuration(remaining), formatETA(eta))
	log.Print(msg)
}

// formatETA returns time as "15:04:05" when today, or "Jan 2 15:04:05" when another day, so past-midnight ETAs aren't confused with "earlier today".
func formatETA(t time.Time) string {
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04:05")
	}
	return t.Format("Jan _2 15:04:05")
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}
