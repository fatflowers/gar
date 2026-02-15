package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestMarkRetryableAndIsRetryable(t *testing.T) {
	t.Parallel()

	if got := MarkRetryable(nil); got != nil {
		t.Fatalf("MarkRetryable(nil) = %v, want nil", got)
	}

	base := errors.New("temporary")
	marked := MarkRetryable(base)
	if !IsRetryableError(marked) {
		t.Fatalf("expected retryable marker on wrapped error")
	}
	if !errors.Is(marked, base) {
		t.Fatalf("expected wrapped error to unwrap to original")
	}
	if got := marked.Error(); got != "temporary" {
		t.Fatalf("unexpected retryable error text: %q", got)
	}

	wrapped := fmt.Errorf("outer: %w", marked)
	if !IsRetryableError(wrapped) {
		t.Fatalf("expected retryable marker to survive wrapping")
	}
	if IsRetryableError(base) {
		t.Fatalf("did not expect plain error to be retryable")
	}
}

func TestNormalizeRetryPolicyDefaultsAndNegative(t *testing.T) {
	t.Parallel()

	got := NormalizeRetryPolicy(RetryPolicy{})
	if got.MaxRetries != defaultRetryMaxRetries {
		t.Fatalf("MaxRetries = %d, want %d", got.MaxRetries, defaultRetryMaxRetries)
	}
	if got.BaseDelay != defaultRetryBaseDelay {
		t.Fatalf("BaseDelay = %v, want %v", got.BaseDelay, defaultRetryBaseDelay)
	}
	if got.MaxDelay != defaultRetryMaxDelay {
		t.Fatalf("MaxDelay = %v, want %v", got.MaxDelay, defaultRetryMaxDelay)
	}

	got = NormalizeRetryPolicy(RetryPolicy{
		MaxRetries: -1,
		BaseDelay:  50 * time.Millisecond,
		MaxDelay:   500 * time.Millisecond,
	})
	if got.MaxRetries != 0 {
		t.Fatalf("negative MaxRetries should disable retries (0), got %d", got.MaxRetries)
	}
	if got.BaseDelay != 50*time.Millisecond {
		t.Fatalf("BaseDelay = %v, want %v", got.BaseDelay, 50*time.Millisecond)
	}
	if got.MaxDelay != 500*time.Millisecond {
		t.Fatalf("MaxDelay = %v, want %v", got.MaxDelay, 500*time.Millisecond)
	}
}

func TestMergeRetryPolicyOverrideAndClamp(t *testing.T) {
	t.Parallel()

	base := RetryPolicy{
		MaxRetries: 1,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   20 * time.Millisecond,
	}

	merged := MergeRetryPolicy(base, RetryPolicy{
		MaxRetries: 2,
		BaseDelay:  30 * time.Millisecond,
	})
	if merged.MaxRetries != 2 {
		t.Fatalf("MaxRetries = %d, want 2", merged.MaxRetries)
	}
	if merged.BaseDelay != 30*time.Millisecond {
		t.Fatalf("BaseDelay = %v, want %v", merged.BaseDelay, 30*time.Millisecond)
	}
	if merged.MaxDelay != 30*time.Millisecond {
		t.Fatalf("MaxDelay should clamp to BaseDelay when smaller, got %v", merged.MaxDelay)
	}

	merged = MergeRetryPolicy(base, RetryPolicy{})
	if merged.MaxRetries != 1 || merged.BaseDelay != 10*time.Millisecond || merged.MaxDelay != 20*time.Millisecond {
		t.Fatalf("unexpected merge result with empty override: %#v", merged)
	}
}

func TestComputeBackoffDelayInRange(t *testing.T) {
	t.Parallel()

	policy := RetryPolicy{
		MaxRetries: 3,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   500 * time.Millisecond,
	}

	assertDelayRange := func(attempt int, nominal time.Duration) {
		t.Helper()
		got := ComputeBackoffDelay(policy, attempt)
		lower := nominal * 8 / 10
		upper := nominal*12/10 + time.Nanosecond
		if got < lower || got > upper {
			t.Fatalf("attempt %d delay out of range: got %v, want [%v, %v]", attempt, got, lower, upper)
		}
	}

	assertDelayRange(0, 100*time.Millisecond)
	assertDelayRange(1, 200*time.Millisecond)
	assertDelayRange(2, 400*time.Millisecond)
	assertDelayRange(4, 500*time.Millisecond)
}

func TestSleepContextCanceledAndSuccess(t *testing.T) {
	t.Parallel()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SleepContext(canceledCtx, 100*time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("SleepContext(cancelled) error = %v, want %v", err, context.Canceled)
	}

	if err := SleepContext(context.Background(), 2*time.Millisecond); err != nil {
		t.Fatalf("SleepContext(background) error = %v", err)
	}
}
