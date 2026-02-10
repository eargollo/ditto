package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIsBusy(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("other"), false},
		{errors.New("database is locked (5) (SQLITE_BUSY)"), true},
		{errors.New("SQLITE_BUSY"), true},
		{errors.New("database is locked"), true},
	}
	for _, tt := range tests {
		got := IsBusy(tt.err)
		if got != tt.want {
			t.Errorf("IsBusy(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestRetryOnBusy_success(t *testing.T) {
	ctx := context.Background()
	n := 0
	err := RetryOnBusy(ctx, 3, 1*time.Millisecond, func() error {
		n++
		if n < 2 {
			return errors.New("database is locked (5) (SQLITE_BUSY)")
		}
		return nil
	})
	if err != nil {
		t.Errorf("RetryOnBusy: %v", err)
	}
	if n != 2 {
		t.Errorf("fn called %d times, want 2", n)
	}
}

func TestRetryOnBusy_nonBusyReturnsImmediately(t *testing.T) {
	ctx := context.Background()
	want := errors.New("other error")
	err := RetryOnBusy(ctx, 5, 1*time.Millisecond, func() error { return want })
	if err != want {
		t.Errorf("RetryOnBusy: got %v", err)
	}
}

func TestRetryOnBusy_contextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RetryOnBusy(ctx, 5, 10*time.Second, func() error {
		return errors.New("database is locked (SQLITE_BUSY)")
	})
	if err != context.Canceled {
		t.Errorf("RetryOnBusy: got %v, want context.Canceled", err)
	}
}
