package db

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"
)

// ErrBusy is returned when a retry budget is exhausted after SQLITE_BUSY.
var ErrBusy = errors.New("database busy: retries exhausted")

// IsBusy reports whether err indicates SQLite returned SQLITE_BUSY (database locked).
// Used to decide when to retry an operation.
func IsBusy(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "SQLITE_BUSY") || strings.Contains(s, "database is locked")
}

// busyRetryCount counts how many times RetryOnBusy slept due to SQLITE_BUSY (for investigation).
var busyRetryCount atomic.Int64

// BusyRetryCount returns the total busy retries since last reset (for logging at end of hash phase).
func BusyRetryCount() int64 {
	return busyRetryCount.Load()
}

// ResetBusyRetryCount resets the busy retry counter (call at start of hash phase).
func ResetBusyRetryCount() {
	busyRetryCount.Store(0)
}

// RetryOnBusy runs fn. If fn returns an error for which IsBusy is true, it backs off
// and retries up to maxAttempts times (including the first run). Backoff doubles each time
// (capped at 5s). Respects context cancellation. Returns the last busy error if all
// attempts hit a busy error (caller can use errors.Is(err, ErrBusy) to detect retry exhaustion).
func RetryOnBusy(ctx context.Context, maxAttempts int, initialBackoff time.Duration, fn func() error) error {
	var lastErr error
	backoff := initialBackoff
	for attempt := 0; attempt < maxAttempts; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !IsBusy(lastErr) {
			return lastErr
		}
		if attempt == maxAttempts-1 {
			break
		}
		busyRetryCount.Add(1)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
		if backoff > 5*time.Second {
			backoff = 5 * time.Second
		}
	}
	// Return last error so logs stay informative; caller can check IsBusy(lastErr).
	return lastErr
}
