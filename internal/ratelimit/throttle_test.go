package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/ratelimit"
)

func TestThrottlerPacesRequestsPerTier(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	throttler := ratelimit.NewThrottler(
		ratelimit.WithClock(clock),
		ratelimit.WithTierInterval(ratelimit.Tier4, time.Second),
	)

	if err := throttler.Wait(context.Background(), ratelimit.Tier4); err != nil {
		t.Fatalf("first Wait returned error: %v", err)
	}
	if len(clock.sleeps) != 0 {
		t.Fatalf("first Wait slept: %#v", clock.sleeps)
	}

	if err := throttler.Wait(context.Background(), ratelimit.Tier4); err != nil {
		t.Fatalf("second Wait returned error: %v", err)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != time.Second {
		t.Fatalf("sleeps = %#v, want one 1s sleep", clock.sleeps)
	}
}

func TestThrottlerCanBeDisabled(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	throttler := ratelimit.NewThrottler(
		ratelimit.WithClock(clock),
		ratelimit.WithDisabled(true),
		ratelimit.WithTierInterval(ratelimit.Tier1, time.Minute),
	)

	if err := throttler.Wait(context.Background(), ratelimit.Tier1); err != nil {
		t.Fatalf("first Wait returned error: %v", err)
	}
	if err := throttler.Wait(context.Background(), ratelimit.Tier1); err != nil {
		t.Fatalf("second Wait returned error: %v", err)
	}
	if len(clock.sleeps) != 0 {
		t.Fatalf("disabled throttler slept: %#v", clock.sleeps)
	}
}

type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return ctx.Err()
}
