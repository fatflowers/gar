package core

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

const (
	defaultRetryMaxRetries = 3
	defaultRetryBaseDelay  = 300 * time.Millisecond
	defaultRetryMaxDelay   = 5 * time.Second
)

// retryableError marks an error as safe to retry by upstream retry loops.
type retryableError struct {
	err error
}

func (e retryableError) Error() string {
	return e.err.Error()
}

func (e retryableError) Unwrap() error {
	return e.err
}

// MarkRetryable wraps an error so retry logic can detect retriable failures.
func MarkRetryable(err error) error {
	if err == nil {
		return nil
	}
	return retryableError{err: err}
}

// IsRetryableError reports whether err has been marked as retryable.
func IsRetryableError(err error) bool {
	var target retryableError
	return errors.As(err, &target)
}

// NormalizeRetryPolicy fills unset retry settings with defaults.
// A negative MaxRetries explicitly disables retries (set to 0).
// A zero MaxRetries is treated as unset and filled with the default.
func NormalizeRetryPolicy(policy RetryPolicy) RetryPolicy {
	if policy.MaxRetries < 0 {
		policy.MaxRetries = 0
	} else if policy.MaxRetries == 0 {
		policy.MaxRetries = defaultRetryMaxRetries
	}
	if policy.BaseDelay <= 0 {
		policy.BaseDelay = defaultRetryBaseDelay
	}
	if policy.MaxDelay <= 0 {
		policy.MaxDelay = defaultRetryMaxDelay
	}
	return policy
}

// MergeRetryPolicy overlays request-level retry options on top of provider defaults.
func MergeRetryPolicy(base RetryPolicy, override RetryPolicy) RetryPolicy {
	merged := NormalizeRetryPolicy(base)
	if override.MaxRetries > 0 {
		merged.MaxRetries = override.MaxRetries
	}
	if override.BaseDelay > 0 {
		merged.BaseDelay = override.BaseDelay
	}
	if override.MaxDelay > 0 {
		merged.MaxDelay = override.MaxDelay
	}
	if merged.MaxDelay < merged.BaseDelay {
		merged.MaxDelay = merged.BaseDelay
	}
	return merged
}

// ComputeBackoffDelay returns exponential backoff with jitter for a retry attempt.
func ComputeBackoffDelay(policy RetryPolicy, attempt int) time.Duration {
	delay := policy.BaseDelay
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= policy.MaxDelay {
			delay = policy.MaxDelay
			break
		}
	}
	if delay > policy.MaxDelay {
		delay = policy.MaxDelay
	}
	jitter := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(delay) * jitter)
}

// SleepContext waits for delay unless the context is canceled first.
func SleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
