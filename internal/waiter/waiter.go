// Package waiter polls the API until an object or a task reaches the state a resource waits
// for. Every wait is bounded by its context (the resource's timeout) and backs off between
// polls, from 5 s to 30 s by default.
package waiter

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Default polling intervals.
const (
	DefaultMinInterval = 5 * time.Second
	DefaultMaxInterval = 30 * time.Second
	DefaultFactor      = 1.5
)

// Config sets how fast a waiter polls. The zero value polls at the defaults.
type Config struct {
	MinInterval time.Duration
	MaxInterval time.Duration
	Factor      float64
}

func (c Config) normalized() Config {
	if c.MinInterval <= 0 {
		c.MinInterval = DefaultMinInterval
	}
	if c.MaxInterval < c.MinInterval {
		c.MaxInterval = max(DefaultMaxInterval, c.MinInterval)
	}
	if c.Factor < 1 {
		c.Factor = DefaultFactor
	}
	return c
}

func (c Config) next(current time.Duration) time.Duration {
	grown := time.Duration(float64(current) * c.Factor)
	return min(grown, c.MaxInterval)
}

// Refresh reads the object and returns it with its current state.
type Refresh[T any] func(ctx context.Context) (T, string, error)

// Wait describes one wait: poll Refresh until it reports a Target state.
type Wait[T any] struct {
	// Description names what is awaited in messages ("instance abc to become active").
	Description string
	// Pending lists the states that keep the wait going. When empty, every state that is not a
	// target keeps it going.
	Pending []string
	Target  []string
	Refresh Refresh[T]
	Config  Config
}

// UnexpectedStateError is returned when Refresh reports a state that is neither pending nor a
// target.
type UnexpectedStateError struct {
	Description string
	State       string
	Expected    []string
}

func (e *UnexpectedStateError) Error() string {
	return fmt.Sprintf("waiting for %s: unexpected state %q, expected %s", e.Description, e.State,
		strings.Join(e.Expected, " or "))
}

// TimeoutError is returned when the context expires before a target state is reached.
type TimeoutError struct {
	Description string
	LastState   string
	Cause       error
}

func (e *TimeoutError) Error() string {
	reason := "timed out"
	if errors.Is(e.Cause, context.Canceled) {
		reason = "interrupted"
	}
	if e.LastState == "" {
		return fmt.Sprintf("%s waiting for %s", reason, e.Description)
	}
	return fmt.Sprintf("%s waiting for %s (last state: %s)", reason, e.Description, e.LastState)
}

func (e *TimeoutError) Unwrap() error { return e.Cause }

// Run polls until a target state, an unexpected state, a refresh error or the end of ctx. It
// refreshes once immediately, then after each interval.
func (w Wait[T]) Run(ctx context.Context) (T, error) {
	cfg := w.Config.normalized()
	interval := cfg.MinInterval
	var (
		zero      T
		lastState string
	)
	for {
		value, state, err := w.Refresh(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return zero, &TimeoutError{Description: w.Description, LastState: lastState, Cause: ctxErr}
			}
			return zero, fmt.Errorf("waiting for %s: %w", w.Description, err)
		}
		lastState = state
		if slices.Contains(w.Target, state) {
			return value, nil
		}
		if len(w.Pending) > 0 && !slices.Contains(w.Pending, state) {
			return value, &UnexpectedStateError{Description: w.Description, State: state, Expected: w.Target}
		}
		tflog.Debug(ctx, "waiting", map[string]any{"for": w.Description, "state": state, "next_poll_in": interval.String()})

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, &TimeoutError{Description: w.Description, LastState: lastState, Cause: ctx.Err()}
		case <-timer.C:
		}
		interval = cfg.next(interval)
	}
}

// Retry calls op until it succeeds, fails with an error retryable does not accept, or ctx ends.
// It is meant for operations the API refuses while something else is in progress (409, an
// object still in use).
func Retry(ctx context.Context, cfg Config, description string, op func(context.Context) error, retryable func(error) bool) error {
	cfg = cfg.normalized()
	interval := cfg.MinInterval
	var lastRetryable error
	for {
		err := op(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil && lastRetryable != nil {
			return fmt.Errorf("%s: gave up when the timeout expired, last error: %w", description, lastRetryable)
		}
		if !retryable(err) {
			return err
		}
		lastRetryable = err
		tflog.Debug(ctx, "retrying", map[string]any{"operation": description, "error": err.Error(), "next_attempt_in": interval.String()})
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%s: gave up when the timeout expired, last error: %w", description, err)
		case <-timer.C:
		}
		interval = cfg.next(interval)
	}
}
