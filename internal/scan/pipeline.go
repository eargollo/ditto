package scan

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eargollo/ditto/internal/db"
	"golang.org/x/time/rate"
)

const (
	defaultNumWalkers   = 4
	defaultNumWriters   = 2
	defaultBatchSize    = 500
	// Small default to surface concurrency issues during testing; override with env for large runs.
	defaultFileChanCap = 1000
	scanProgressLogIntervalPipeline = 1000
	scanProgressWriterLogInterval   = 5000
	scanProgressUpdateInterval      = 2 * time.Second
	scanDebugHeartbeatInterval      = 5 * time.Second
)

// DebugPipelineEnv enables pipeline debug logging (heartbeat + stuck detection). Set to 1 or true.
const DebugPipelineEnv = "DITTO_DEBUG_PIPELINE"

// pipelineChanCaps returns (fileCap) from env or defaults. Dir queue is unbounded; only file channel is sized.
func pipelineChanCaps() (fileCap int) {
	fileCap = defaultFileChanCap
	if s := os.Getenv("DITTO_SCAN_FILE_CHAN_CAP"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			fileCap = n
		}
	}
	return fileCap
}

// dirQueue is an unbounded queue of directory paths: producers Push (never block on capacity),
// consumers receive from Out(). Uses mutex+slice+cond so no fixed buffer can fill and cause deadlock.
type dirQueue struct {
	mu     sync.Mutex
	slice  []string
	closed bool
	cond   *sync.Cond
	outChan chan string
}

func newDirQueue() *dirQueue {
	q := &dirQueue{outChan: make(chan string)}
	q.cond = sync.NewCond(&q.mu)
	go q.run()
	return q
}

func (q *dirQueue) run() {
	for {
		q.mu.Lock()
		for len(q.slice) == 0 && !q.closed {
			q.cond.Wait()
		}
		if q.closed && len(q.slice) == 0 {
			q.mu.Unlock()
			close(q.outChan)
			return
		}
		dir := q.slice[0]
		q.slice = q.slice[1:]
		q.mu.Unlock()
		q.outChan <- dir
	}
}

// Push adds a dir; never blocks on buffer capacity (unbounded slice).
func (q *dirQueue) Push(dir string) {
	q.mu.Lock()
	q.slice = append(q.slice, dir)
	q.cond.Signal()
	q.mu.Unlock()
}

// Close signals no more Push; call after wg.Wait() so the run goroutine can drain and close outChan.
func (q *dirQueue) Close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

// Out returns the channel walkers receive from (closed when queue is closed and drained).
func (q *dirQueue) Out() <-chan string { return q.outChan }

// Len returns the number of dirs currently in the queue (for periodic scan info).
func (q *dirQueue) Len() int {
	q.mu.Lock()
	n := len(q.slice)
	q.mu.Unlock()
	return n
}

func debugPipeline() bool {
	s := os.Getenv(DebugPipelineEnv)
	return s == "1" || s == "true" || s == "yes"
}

// Env names for pipeline tuning (e.g. on Synology NAS). Unset = use default.
const (
	EnvScanWalkers   = "DITTO_SCAN_WALKERS"   // number of walker goroutines (default 4)
	EnvScanWriters   = "DITTO_SCAN_WRITERS"   // number of writer goroutines (default 2)
	EnvScanBatchSize = "DITTO_SCAN_BATCH_SIZE" // max entries per DB batch (default 500)
)

// pipelineConfigFromEnv returns a PipelineConfig from environment variables. Use when config is nil (e.g. from UI).
// Recommended on Synology: DITTO_SCAN_WALKERS=2 DITTO_SCAN_WRITERS=1 DITTO_SCAN_BATCH_SIZE=250 to reduce CPU/memory.
func pipelineConfigFromEnv() *PipelineConfig {
	c := &PipelineConfig{}
	if s := os.Getenv(EnvScanWalkers); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			c.NumWalkers = n
		}
	}
	if s := os.Getenv(EnvScanWriters); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			c.NumWriters = n
		}
	}
	if s := os.Getenv(EnvScanBatchSize); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			c.BatchSize = n
		}
	}
	return c
}

// PipelineConfig configures the parallel scan pipeline.
type PipelineConfig struct {
	NumWalkers int // number of goroutines that list directories and emit files
	NumWriters int // number of goroutines that batch and write to DB
	BatchSize  int // max entries per DB batch (0 = defaultBatchSize)
}

func (c *PipelineConfig) numWalkers() int {
	if c != nil && c.NumWalkers > 0 {
		return c.NumWalkers
	}
	return defaultNumWalkers
}

func (c *PipelineConfig) numWriters() int {
	if c != nil && c.NumWriters > 0 {
		return c.NumWriters
	}
	return defaultNumWriters
}

func (c *PipelineConfig) batchSize() int {
	if c != nil && c.BatchSize > 0 {
		return c.BatchSize
	}
	return defaultBatchSize
}

// ScanMetrics holds instrumentation for the scan pipeline (FS vs DB time and counts).
type ScanMetrics struct {
	FsNanos       atomic.Int64 // cumulative nanoseconds in walkers (listing + Lstat)
	DbNanos       atomic.Int64 // cumulative nanoseconds in writers (batch inserts)
	FilesWalked   atomic.Int64 // files emitted by walkers
	FilesWritten  atomic.Int64 // files written to DB by writers
	DirsProcessed atomic.Int64
	Skipped       atomic.Int64 // paths skipped (permission or exclude)
	FileQueueLen  atomic.Int64 // number of entries currently in fileChan (increment on send, decrement on receive)
	StartTime     time.Time    // when the pipeline started (for progress rate)
}

func (m *ScanMetrics) Log() {
	fsSec := time.Duration(m.FsNanos.Load()).Seconds()
	dbSec := time.Duration(m.DbNanos.Load()).Seconds()
	log.Printf("[scan] metrics: fs=%.2fs db=%.2fs files_walked=%d files_written=%d dirs=%d skipped=%d",
		fsSec, dbSec, m.FilesWalked.Load(), m.FilesWritten.Load(), m.DirsProcessed.Load(), m.Skipped.Load())
}

// RunPipeline runs the parallel walk -> batched write pipeline for the given scan.
// It returns the number of files written and skipped, or an error. The scan's completed_at
// is not updated; the caller must call db.UpdateScanCompletedAt.
func RunPipeline(ctx context.Context, database *sql.DB, scanID, folderID int64, rootPath, folderPath string, opts *ScanOptions, config *PipelineConfig) (fileCount, skippedScan int64, metrics *ScanMetrics, err error) {
	if config == nil {
		config = pipelineConfigFromEnv()
	}
	rootPath = filepath.Clean(rootPath)
	patterns := DefaultExcludePatterns()
	if opts != nil && len(opts.ExcludePatterns) > 0 {
		patterns = opts.ExcludePatterns
	}
	maxFilesPerSecond := 0
	if opts != nil {
		maxFilesPerSecond = opts.MaxFilesPerSecond
	}

	fileCap := pipelineChanCaps()
	if debugPipeline() {
		log.Printf("[scan] pipeline: walkers=%d writers=%d batch=%d fileChan=%d (env: DITTO_SCAN_WALKERS, DITTO_SCAN_WRITERS, DITTO_SCAN_BATCH_SIZE, DITTO_SCAN_FILE_CHAN_CAP)",
			config.numWalkers(), config.numWriters(), config.batchSize(), fileCap)
	}
	dirs := newDirQueue()
	fileChan := make(chan Entry, fileCap)
	metrics = &ScanMetrics{StartTime: time.Now()}
	var wg sync.WaitGroup

	// Progress updater: write current file count to DB periodically so the UI shows live progress.
	progressDone := make(chan struct{})
	go runProgressUpdater(ctx, database, scanID, metrics, progressDone)

	// Debug heartbeat: when DITTO_DEBUG_PIPELINE=1, log metrics every 5s and detect stuck (no change).
	debugDone := make(chan struct{})
	if debugPipeline() {
		go runDebugHeartbeat(metrics, dirs, debugDone)
	}

	// Bootstrap: enqueue root (unbounded queue, Push blocks only if 50k dirs already queued).
	wg.Add(1)
	dirs.Push(rootPath)

	// Closer: when all dirs are processed, close dir queue then fileChan so walkers then writers exit.
	go func() {
		wg.Wait()
		dirs.Close()
		close(fileChan)
	}()

	// Start walkers: they consume from dirs.Out() and Push subdirs to dirs (unbounded).
	numWalkers := config.numWalkers()
	for i := 0; i < numWalkers; i++ {
		go runWalker(ctx, rootPath, folderPath, patterns, maxFilesPerSecond, dirs, fileChan, &wg, metrics)
	}

	// Start writers
	numWriters := config.numWriters()
	batchSize := config.batchSize()
	writerDone := make(chan error, numWriters)
	for i := 0; i < numWriters; i++ {
		go runWriterSafe(ctx, database, folderID, scanID, folderPath, fileChan, batchSize, metrics, writerDone)
	}

	// Wait for all writers to finish (they exit when fileChan is closed and drained)
	var firstErr error
	for i := 0; i < numWriters; i++ {
		if e := <-writerDone; e != nil && firstErr == nil {
			firstErr = e
		}
	}
	close(progressDone) // stop progress updater so it doesn't overwrite final count
	if debugPipeline() {
		close(debugDone)
	}
	if firstErr != nil {
		return metrics.FilesWritten.Load(), metrics.Skipped.Load(), metrics, firstErr
	}

	metrics.Log()
	return metrics.FilesWritten.Load(), metrics.Skipped.Load(), metrics, nil
}

// runDebugHeartbeat logs pipeline metrics periodically and logs "possibly stuck" when nothing changes.
func runDebugHeartbeat(metrics *ScanMetrics, dirs *dirQueue, done <-chan struct{}) {
	ticker := time.NewTicker(scanDebugHeartbeatInterval)
	defer ticker.Stop()
	var lastWalked, lastWritten, lastDirs int64
	var sameCount int
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			walked := metrics.FilesWalked.Load()
			written := metrics.FilesWritten.Load()
			dirsProcessed := metrics.DirsProcessed.Load()
			dirQueueLen := dirs.Len()
			fileQueueLen := metrics.FileQueueLen.Load()
			elapsed := time.Since(metrics.StartTime).Seconds()
			log.Printf("[scan] pipeline heartbeat: walked=%d written=%d dirs=%d dir_queue=%d file_queue=%d skipped=%d elapsed=%.1fs",
				walked, written, dirsProcessed, dirQueueLen, fileQueueLen, metrics.Skipped.Load(), elapsed)
			if walked == lastWalked && written == lastWritten && dirsProcessed == lastDirs {
				sameCount++
				if sameCount >= 2 {
					log.Printf("[scan] pipeline possibly stuck (no change for %.0fs): walked=%d written=%d dirs=%d dir_queue=%d file_queue=%d — check for deadlock or blocked channel",
						scanDebugHeartbeatInterval.Seconds()*float64(sameCount), walked, written, dirsProcessed, dirQueueLen, fileQueueLen)
				}
			} else {
				sameCount = 0
			}
			lastWalked, lastWritten, lastDirs = walked, written, dirsProcessed
		}
	}
}

// runProgressUpdater updates the scan row's file_count periodically so the UI shows live progress.
// Exits when progressDone is closed or ctx is cancelled.
func runProgressUpdater(ctx context.Context, database *sql.DB, scanID int64, metrics *ScanMetrics, progressDone <-chan struct{}) {
	ticker := time.NewTicker(scanProgressUpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-progressDone:
			return
		case <-ticker.C:
			n := metrics.FilesWritten.Load()
			if err := db.UpdateScanFileCountProgress(ctx, database, scanID, n); err != nil {
				log.Printf("[scan] progress update: %v", err)
			}
		}
	}
}

// runWalker consumes dirs from the queue, lists each, Pushes subdirs (unbounded), and sends files to fileChan.
func runWalker(ctx context.Context, rootPath, folderPath string, patterns []string, maxFilesPerSecond int,
	dirs *dirQueue, fileChan chan<- Entry, wg *sync.WaitGroup, metrics *ScanMetrics) {
	var limiter *rate.Limiter
	if maxFilesPerSecond > 0 {
		limiter = rate.NewLimiter(rate.Limit(maxFilesPerSecond), 1)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case dir, ok := <-dirs.Out():
			if !ok {
				return
			}
			fsStart := time.Now()
			if err := processOneDir(ctx, dir, rootPath, folderPath, patterns, limiter, dirs, fileChan, wg, metrics); err != nil {
				log.Printf("[scan] walker error at %s: %v", dir, err)
			}
			metrics.FsNanos.Add(time.Since(fsStart).Nanoseconds())
			metrics.DirsProcessed.Add(1)
			wg.Done()
		}
	}
}

func processOneDir(ctx context.Context, dir string, rootPath, folderPath string, patterns []string, limiter *rate.Limiter,
	dirs *dirQueue, fileChan chan<- Entry, wg *sync.WaitGroup, metrics *ScanMetrics) error {
	if os.Getenv(DebugScanEnv) != "" {
		log.Printf("[scan] listing directory: %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if isPermissionOrAccessError(err) {
			metrics.Skipped.Add(1)
			log.Printf("[scan] skipped (permission): %s: %v", dir, err)
			return nil
		}
		return err
	}
	for _, d := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := d.Name()
		fullPath := filepath.Join(dir, name)
		if ShouldExclude(fullPath, patterns) {
			metrics.Skipped.Add(1)
			if d.IsDir() {
				continue
			}
			continue
		}
		if d.IsDir() {
			wg.Add(1)
			dirs.Push(fullPath) // unbounded, never blocks on capacity
			continue
		}
		if d.Type()&fs.ModeSymlink != 0 {
			continue
		}
		if !d.Type().IsRegular() {
			continue
		}
		info, err := os.Lstat(fullPath)
		if err != nil {
			log.Printf("[scan] error at %s (Lstat): %v", fullPath, err)
			return err
		}
		absPath, err := filepath.Abs(fullPath)
		if err != nil {
			log.Printf("[scan] error at %s (Abs): %v", fullPath, err)
			return err
		}
		inode, dev := inodeAndDev(info)
		var deviceID *int64
		if dev != 0 {
			deviceID = &dev
		}
		e := Entry{
			Path:     absPath,
			Size:     info.Size(),
			MTime:    info.ModTime().Unix(),
			Inode:    inode,
			DeviceID: deviceID,
		}
		if limiter != nil {
			if err := limiter.Wait(ctx); err != nil {
				return err
			}
		}
		select {
		case fileChan <- e:
			metrics.FileQueueLen.Add(1)
			metrics.FilesWalked.Add(1)
			n := metrics.FilesWalked.Load()
			if n%scanProgressLogIntervalPipeline == 0 {
				elapsed := time.Since(metrics.StartTime).Seconds()
				rate := float64(n) / elapsed
				dirQueueLen := dirs.Len()
				fileQueueLen := metrics.FileQueueLen.Load()
				if rate > 0 {
					log.Printf("[scan] %d files discovered (%.0f/s, %.1fs elapsed) dir_queue=%d file_queue=%d current: %s", n, rate, elapsed, dirQueueLen, fileQueueLen, absPath)
				} else {
					log.Printf("[scan] %d files discovered (%.1fs elapsed) dir_queue=%d file_queue=%d current: %s", n, elapsed, dirQueueLen, fileQueueLen, absPath)
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// runWriterSafe wraps runWriter with panic recovery so one failed writer doesn't hang the pipeline.
func runWriterSafe(ctx context.Context, database *sql.DB, folderID, scanID int64, folderPath string,
	fileChan <-chan Entry, batchSize int, metrics *ScanMetrics, done chan<- error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[scan] writer panic: %v", r)
			done <- fmt.Errorf("writer panic: %v", r)
		}
	}()
	runWriter(ctx, database, folderID, scanID, folderPath, fileChan, batchSize, metrics, done)
}

// runWriter reads entries from fileChan, batches them, and writes via UpsertFilesBatch + InsertFileScanBatch.
func runWriter(ctx context.Context, database *sql.DB, folderID, scanID int64, folderPath string,
	fileChan <-chan Entry, batchSize int, metrics *ScanMetrics, done chan<- error) {
	batch := make([]Entry, 0, batchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		rows := make([]db.FileRow, len(batch))
		for i, e := range batch {
			relPath, err := filepath.Rel(folderPath, e.Path)
			if err != nil {
				relPath = e.Path
			}
			rows[i] = db.FileRow{
				Path:     relPath,
				Size:     e.Size,
				MTime:    e.MTime,
				Inode:    e.Inode,
				DeviceID: e.DeviceID,
			}
		}
		t0 := time.Now()
		ids, err := db.UpsertFilesBatch(ctx, database, folderID, rows)
		if err != nil {
			return err
		}
		if err := db.InsertFileScanBatch(ctx, database, ids, scanID); err != nil {
			return err
		}
		metrics.DbNanos.Add(time.Since(t0).Nanoseconds())
		prevWritten := metrics.FilesWritten.Load()
		written := metrics.FilesWritten.Add(int64(len(batch)))
		batch = batch[:0]
		// Log when we cross a 5k boundary so user sees writer progress (e.g. when walkers are blocked on full channel).
		if written/scanProgressWriterLogInterval > prevWritten/scanProgressWriterLogInterval {
			elapsed := time.Since(metrics.StartTime).Seconds()
			log.Printf("[scan] %d files written to DB (%.1fs elapsed)", written, elapsed)
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			done <- ctx.Err()
			return
		case e, ok := <-fileChan:
			if !ok {
				if err := flush(); err != nil {
					done <- err
					return
				}
				done <- nil
				return
			}
			metrics.FileQueueLen.Add(-1)
			batch = append(batch, e)
			if len(batch) >= batchSize {
				if err := flush(); err != nil {
					done <- err
					return
				}
			}
		}
	}
}
