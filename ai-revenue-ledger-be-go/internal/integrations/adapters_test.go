package integrations

import (
"context"
"errors"
"testing"
"time"
)

func TestDoWithRetryEventualSuccess(t *testing.T) {
attempts := 0
cfg := RetryConfig{
MaxAttempts: 3,
BaseDelay:   10 * time.Millisecond,
MaxDelay:    50 * time.Millisecond,
}

res, err := DoWithRetry(context.Background(), cfg, func(ctx context.Context) (SyncResult, error) {
attempts++
if attempts < 3 {
return SyncResult{}, ErrSourceDown
}
return SyncResult{
Provider:     "ga4",
Mode:         "initial_90d",
LookbackDays: 90,
RecordsCount: 42,
}, nil
})

if err != nil {
t.Fatalf("expected success after retries, got error: %v", err)
}
if attempts != 3 {
t.Fatalf("expected 3 attempts, got %d", attempts)
}
if res.RecordsCount != 42 {
t.Fatalf("expected 42 records, got %d", res.RecordsCount)
}
}

func TestDoWithRetryStopsOnExpiredToken(t *testing.T) {
attempts := 0
cfg := RetryConfig{
MaxAttempts: 3,
BaseDelay:   10 * time.Millisecond,
MaxDelay:    50 * time.Millisecond,
}

_, err := DoWithRetry(context.Background(), cfg, func(ctx context.Context) (SyncResult, error) {
attempts++
return SyncResult{}, ErrTokenExpired
})

if !errors.Is(err, ErrTokenExpired) {
t.Fatalf("expected ErrTokenExpired, got %v", err)
}
if attempts != 1 {
t.Fatalf("expected 1 attempt on expired token, got %d", attempts)
}
}
