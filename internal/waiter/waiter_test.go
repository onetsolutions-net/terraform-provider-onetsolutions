package waiter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var fast = Config{MinInterval: time.Millisecond, MaxInterval: 2 * time.Millisecond}

func sequence(states ...string) Refresh[int] {
	calls := 0
	return func(context.Context) (int, string, error) {
		state := states[min(calls, len(states)-1)]
		calls++
		return calls, state, nil
	}
}

func TestWaitReachesTarget(t *testing.T) {
	t.Parallel()
	calls, err := Wait[int]{
		Description: "thing", Pending: []string{"building"}, Target: []string{"active"},
		Refresh: sequence("building", "building", "active"), Config: fast,
	}.Run(context.Background())
	if err != nil || calls != 3 {
		t.Fatalf("calls = %d, err = %v", calls, err)
	}
}

func TestWaitWithoutPendingAcceptsAnyIntermediateState(t *testing.T) {
	t.Parallel()
	_, err := Wait[int]{
		Description: "thing", Target: []string{"active"},
		Refresh: sequence("queued", "whatever", "active"), Config: fast,
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestWaitUnexpectedState(t *testing.T) {
	t.Parallel()
	_, err := Wait[int]{
		Description: "instance x", Pending: []string{"building"}, Target: []string{"active"},
		Refresh: sequence("building", "error"), Config: fast,
	}.Run(context.Background())
	var unexpected *UnexpectedStateError
	if !errors.As(err, &unexpected) || unexpected.State != "error" {
		t.Fatalf("err = %v", err)
	}
}

func TestWaitTimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := Wait[int]{
		Description: "instance x", Target: []string{"active"},
		Refresh: sequence("building"), Config: fast,
	}.Run(ctx)
	var timeout *TimeoutError
	if !errors.As(err, &timeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "timed out waiting for instance x (last state: building)") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestWaitRefreshError(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	_, err := Wait[int]{
		Description: "x", Target: []string{"active"}, Config: fast,
		Refresh: func(context.Context) (int, string, error) { return 0, "", boom },
	}.Run(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestBackoffGrowsToTheMaximum(t *testing.T) {
	t.Parallel()
	cfg := Config{}.normalized()
	if cfg.MinInterval != DefaultMinInterval || cfg.MaxInterval != DefaultMaxInterval {
		t.Fatalf("defaults = %+v", cfg)
	}
	interval := cfg.MinInterval
	for range 20 {
		interval = cfg.next(interval)
	}
	if interval != DefaultMaxInterval {
		t.Fatalf("interval = %s", interval)
	}
}

func TestRetry(t *testing.T) {
	t.Parallel()
	inUse := errors.New("in use")
	attempts := 0
	err := Retry(context.Background(), fast, "deleting", func(context.Context) error {
		attempts++
		if attempts < 3 {
			return inUse
		}
		return nil
	}, func(err error) bool { return errors.Is(err, inUse) })
	if err != nil || attempts != 3 {
		t.Fatalf("attempts = %d, err = %v", attempts, err)
	}

	fatal := errors.New("fatal")
	err = Retry(context.Background(), fast, "x", func(context.Context) error { return fatal },
		func(err error) bool { return errors.Is(err, inUse) })
	if !errors.Is(err, fatal) {
		t.Fatalf("err = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err = Retry(ctx, fast, "deleting", func(context.Context) error { return inUse },
		func(error) bool { return true })
	if !errors.Is(err, inUse) || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("err = %v", err)
	}
}
