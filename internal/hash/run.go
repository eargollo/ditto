package hash

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eargollo/ditto/internal/db"
	"golang.org/x/time/rate"
)

const hashProgressLogInterval = 50                  // log "N/M files" every this many files (scaled up for large totals below)
const hashProgressUpdateInterval = 2 * time.Second  // write hash stats to DB for live UI (scan progress page polls every 2s)
const slowOpThreshold = 100 * time.Millisecond      // log when a single DB op exceeds this (for investigation)
const hashJobChannelCap = 1000                     // bounded channel for producer-consumer; backpressure if consumers are slow
const fileLogInterval = 5 * time.Second            // at most one per-file log line every this long (avoid flooding)
const hashBatchSize = 50                            // flush hash updates to DB in batches to reduce round-trips (e.g. on NAS)

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
// workerID is -1 to omit worker from the log; otherwise logs "worker N" so you can see parallelism.
func logFileIfThrottled(workerID int, format string, args ...interface{}) {
	fileLogMu.Lock()
	defer fileLogMu.Unlock()
	if time.Since(fileLogLastTime) < fileLogInterval {
		return
	}
	fileLogLastTime = time.Now()
	if workerID >= 0 {
		// Insert "worker N" after "[hash] " so we get "[hash] worker 2 hashing ..."
		format = "[hash] worker %d " + strings.TrimPrefix(format, "[hash] ")
		args = append([]interface{}{workerID}, args...)
	}
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
func RunHashPhase(ctx context.Context, q db.Querier, scanID int64, opts *HashOptions) error {
	if err := db.ResetHashStatusHashingToPendingGlobal(ctx, q); err != nil {
		return err
	}
	if err := db.UpdateScanHashStartedAt(ctx, q, scanID); err != nil {
		return err
	}
	// Files already 'done' in this scan were not set to pending (mtime unchanged); count them as reused (no read).
	initialDoneCount, _, _ := db.GetHashedFileCountAndBytes(ctx, q, scanID)
	// Use global candidate count so we also hash pending files from previous scans whose size is now duplicate (e.g. scenario 8).
	total, _ := db.CountHashCandidatesGlobal(ctx, q)
	n := opts.workers()
	log.Printf("[hash] phase started for scan %d (%d worker(s), %d files to hash)", scanID, n, total)
	phaseStart := time.Now().UTC()
	var completed, reusedCount, hashErrorCount atomic.Int64
	err := runHashPhaseProducerConsumer(ctx, q, scanID, total, initialDoneCount, &completed, &reusedCount, &hashErrorCount, phaseStart, opts, n)
	if err != nil {
		log.Printf("[hash] phase failed for scan %d: %v", scanID, err)
		return err
	}
	totalReused := initialDoneCount + reusedCount.Load()
	errCount := hashErrorCount.Load()
	fileCount, byteCount, _ := db.GetHashedFileCountAndBytes(ctx, q, scanID)
	log.Printf("[hash] phase completed for scan %d: %d files, %d bytes, %d reused, %d errors", scanID, fileCount, byteCount, totalReused, errCount)
	// Always update the triggering scan so hash_completed_at is set even when 0 files hashed (e.g. scan 1 with unique sizes).
	_ = db.UpdateScanHashStats(ctx, q, scanID, fileCount, byteCount, totalReused, errCount)
	// Refresh hash stats for other scans that have hashed files (we may have hashed files from previous scans in this phase).
	scanIDs, _ := db.ScanIDsWithHashedFiles(ctx, q)
	for _, sid := range scanIDs {
		if sid == scanID {
			continue
		}
		c, b, err := db.GetHashedFileCountAndBytes(ctx, q, sid)
		if err != nil {
			continue
		}
		_ = db.UpdateScanHashStats(ctx, q, sid, c, b, 0, 0)
	}
	if err := db.PrecomputeDuplicateGroupsHash(ctx, q); err != nil {
		log.Printf("[hash] precompute duplicate groups failed: %v", err)
	}
	return nil
}

// runHashProgressUpdater writes hashed_file_count, hashed_byte_count, hash_reused_count, hash_error_count
// to the scan row periodically so the UI shows live progress. Exits when progressDone is closed or ctx is cancelled.
// initialReused is added to reusedCount so "reused" includes files that were already done (not queued).
func runHashProgressUpdater(ctx context.Context, q db.Querier, scanID int64, progressDone <-chan struct{}, initialReused int64, reusedCount, hashErrorCount *atomic.Int64) {
	ticker := time.NewTicker(hashProgressUpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-progressDone:
			return
		case <-ticker.C:
			fileCount, byteCount, err := db.GetHashedFileCountAndBytes(ctx, q, scanID)
			if err != nil {
				continue
			}
			totalReused := initialReused + reusedCount.Load()
			_ = db.UpdateScanHashProgress(ctx, q, scanID, fileCount, byteCount, totalReused, hashErrorCount.Load())
		}
	}
}

// runHashPhaseProducerConsumer: one producer sends pending jobs to a bounded channel;
// N consumers process jobs and send hash results to a batch writer; writer flushes to DB in batches.
// initialReused is the count of files in this scan already 'done' (unchanged mtime); added to reusedCount for display.
func runHashPhaseProducerConsumer(ctx context.Context, q db.Querier, scanID int64, total int64, initialReused int64, completed, reusedCount, hashErrorCount *atomic.Int64, phaseStart time.Time, opts *HashOptions, numWorkers int) error {
	jobs := make(chan *db.File, hashJobChannelCap)
	results := make(chan db.HashUpdate, hashJobChannelCap)
	errCh := make(chan error, 1)
	progressDone := make(chan struct{})

	// Live progress: update scan row periodically so the UI shows hash stats (like scan file_count).
	go runHashProgressUpdater(ctx, q, scanID, progressDone, initialReused, reusedCount, hashErrorCount)

	// Producer: stream pending jobs from any scan (global queue). Deduplicate by file ID so we hash each file only once (same file can appear in multiple scans).
	go func() {
		defer close(jobs)
		seen := make(map[int64]struct{})
		err := db.ForEachPendingHashJobGlobal(ctx, q, func(f *db.File) error {
			if _, ok := seen[f.ID]; ok {
				return nil
			}
			seen[f.ID] = struct{}{}
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

	// Batch writer: collect hash updates and flush in batches to reduce DB round-trips (e.g. on NAS).
	var writerWg sync.WaitGroup
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		var batch []db.HashUpdate
		flush := func() {
			if len(batch) == 0 {
				return
			}
			t0 := time.Now()
			if err := db.UpdateFileHashBatch(ctx, q, batch); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			logSlowIf("UpdateFileHashBatch", t0)
			batch = nil
		}
		for {
			select {
			case r, ok := <-results:
				if !ok {
					flush()
					return
				}
				batch = append(batch, r)
				if len(batch) >= hashBatchSize {
					flush()
				}
			case <-ctx.Done():
				flush()
				return
			}
		}
	}()

	now := time.Now().UTC()
	var limiter *rate.Limiter
	if opts != nil && opts.MaxHashesPerSecond > 0 {
		limiter = rate.NewLimiter(rate.Limit(opts.MaxHashesPerSecond), 1)
	}
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				reused, update, err := processClaimedJob(ctx, q, job, opts, now, limiter, workerID)
				if err != nil {
					if hashErrorCount != nil {
						hashErrorCount.Add(1)
					}
					_ = db.ResetFileHashStatusToPending(ctx, q, job.ID)
					select {
					case errCh <- err:
					default:
					}
					return
				}
				if reused && reusedCount != nil {
					reusedCount.Add(1)
				}
				select {
				case results <- *update:
				case <-ctx.Done():
					return
				}
				progressLog(completed, total, phaseStart, numWorkers)
			}
		}()
	}

	wg.Wait()
	close(results)
	writerWg.Wait()
	close(progressDone)

	select {
	case runErr := <-errCh:
		return runErr
	default:
		return nil
	}
}

// processClaimedJob hashes the file (or reuses inode/previous hash). Returns (reused, update, nil) on success; update is sent to batch writer.
// workerID is used in throttled per-file logs so you can see which worker handled the file (0 to numWorkers-1).
func processClaimedJob(ctx context.Context, q db.Querier, job *db.File, opts *HashOptions, now time.Time, limiter *rate.Limiter, workerID int) (reused bool, update *db.HashUpdate, err error) {
	var h string
	// If this file's hash was cleared (set to pending because mtime/size changed), we must re-read from disk.
	// Otherwise we could reuse a stale hash and miss content changes (e.g. scenario 3: B same size, different content).
	if job.Hash != nil {
		// Same-scan inode reuse (hardlink)
		t0 := time.Now()
		h, err = db.HashForInode(ctx, q, job.ScanID, job.Inode, job.DeviceID)
		logSlowIf("HashForInode", t0)
		if err != nil {
			return false, nil, err
		}
		if h != "" {
			logFileIfThrottled(workerID, "[hash] reused (inode) %s [%s]", job.Path, filepath.Base(job.Path))
			return true, &db.HashUpdate{FileID: job.ID, Hash: h, HashedAt: now, Mtime: job.MTime}, nil
		}
		// Previous-scan unchanged file reuse (inode+device+size)
		t2 := time.Now()
		h, err = db.HashForInodeFromPreviousScan(ctx, q, job.ScanID, job.Inode, job.DeviceID, job.Size)
		logSlowIf("HashForInodeFromPreviousScan", t2)
		if err != nil {
			return false, nil, err
		}
		if h != "" {
			logFileIfThrottled(workerID, "[hash] reused (unchanged) %s [%s]", job.Path, filepath.Base(job.Path))
			return true, &db.HashUpdate{FileID: job.ID, Hash: h, HashedAt: now, Mtime: job.MTime}, nil
		}
		// When inode is nil (e.g. Windows/network), reuse by path+size from any file that already has a hash.
		if job.Inode == nil && job.RelPath != "" {
			t3 := time.Now()
			h, err = db.HashForPathSize(ctx, q, job.RelPath, job.Size)
			logSlowIf("HashForPathSize", t3)
			if err != nil {
				return false, nil, err
			}
			if h != "" {
				logFileIfThrottled(workerID, "[hash] reused (path+size) %s [%s]", job.Path, filepath.Base(job.Path))
				return true, &db.HashUpdate{FileID: job.ID, Hash: h, HashedAt: now, Mtime: job.MTime}, nil
			}
		}
	}
	// Throttle before reading
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return false, nil, err
		}
	}
	logFileIfThrottled(workerID, "[hash] hashing %s [%s] (%d bytes)", job.Path, filepath.Base(job.Path), job.Size)
	h, err = HashFile(job.Path)
	if err != nil {
		logFileIfThrottled(workerID, "[hash] failed %s [%s]: %v", job.Path, filepath.Base(job.Path), err)
		return false, nil, err
	}
	logFileIfThrottled(workerID, "[hash] hashed %s [%s]", job.Path, filepath.Base(job.Path))
	return false, &db.HashUpdate{FileID: job.ID, Hash: h, HashedAt: now, Mtime: job.MTime}, nil
}

// progressLog logs "N/M files (X%)" and optionally ETA. Interval scales with total so we don't flood logs (e.g. every 5k when 169k files).
// Rate = n/elapsed from start; remaining = (total-n)/rate; ETA = now+remaining. All values kept non-negative.
func progressLog(completed *atomic.Int64, total int64, phaseStart time.Time, numWorkers int) {
	if total <= 0 {
		return
	}
	n := completed.Add(1)
	interval := int64(hashProgressLogInterval)
	if total > 100000 {
		interval = 5000
	} else if total > 10000 {
		interval = 1000
	} else if total > 1000 {
		interval = 500
	}
	if n%interval != 0 && n != total {
		return
	}
	// Cap at total so we never show >100% or negative remaining when n races past total.
	displayN := n
	if displayN > total {
		displayN = total
	}
	pct := float64(100) * float64(displayN) / float64(total)
	msg := fmt.Sprintf("[hash] progress: %d/%d files (%.1f%%)", displayN, total, pct)
	if numWorkers > 1 {
		msg += fmt.Sprintf(" (%d workers)", numWorkers)
	}
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
