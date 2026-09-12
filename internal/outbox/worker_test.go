package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryPolicy(t *testing.T) {
	if MaxAttempts != 3 || RetryDelay(1) != time.Minute || RetryDelay(2) != 5*time.Minute || RetryDelay(99) != 5*time.Minute {
		t.Fatal("unbounded retry policy")
	}
	if LeaseDuration <= SendTimeout || PollInterval < time.Second || PollInterval > 5*time.Second {
		t.Fatal("unsafe timing policy")
	}
}
func TestRunWaitsAfterEmptyQueueAndErrors(t *testing.T) {
	for _, processErr := range []error{nil, errors.New("database unavailable")} {
		ctx, cancel := context.WithCancel(t.Context())
		calls, waits := 0, 0
		err := run(ctx, func(context.Context) (bool, error) { calls++; return false, processErr }, func(context.Context) bool {
			waits++
			if calls != waits {
				t.Fatal("busy loop")
			}
			if waits == 3 {
				cancel()
				return false
			}
			return true
		})
		cancel()
		if err != nil || calls != 3 || waits != 3 {
			t.Fatal("polling policy")
		}
	}
}
func TestRunDrainsAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	err := run(ctx, func(context.Context) (bool, error) {
		calls++
		if calls == 4 {
			cancel()
		}
		return true, nil
	}, func(context.Context) bool { t.Fatal("wait while work available"); return false })
	if err != nil || calls != 4 {
		t.Fatal("shutdown")
	}
	calls = 0
	if err = run(ctx, func(context.Context) (bool, error) { calls++; return false, nil }, waitPoll); err != nil || calls != 0 {
		t.Fatal("work after cancellation")
	}
	if waitPoll(ctx) {
		t.Fatal("idle shutdown")
	}
}
