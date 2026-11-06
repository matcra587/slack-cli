package ratelimit_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
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

func TestThrottlerConcurrentCallsKeepSpacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		throttler := ratelimit.NewThrottler(ratelimit.WithTierInterval(ratelimit.Tier4, time.Second))
		admitted := make(chan time.Time, 12)
		var calls sync.WaitGroup
		for range 12 {
			calls.Go(func() {
				if err := throttler.Wait(t.Context(), ratelimit.Tier4); err != nil {
					t.Error(err)
					return
				}
				admitted <- time.Now()
			})
		}
		calls.Wait()
		close(admitted)
		times := []time.Time{}
		for at := range admitted {
			times = append(times, at)
		}
		if len(times) != 12 {
			t.Fatalf("admitted %d requests, want 12", len(times))
		}
		slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
		for i := 1; i < len(times); i++ {
			if gap := times[i].Sub(times[i-1]); gap < time.Second {
				t.Fatalf("request %d admitted after %s, want >=1s", i, gap)
			}
		}
	})
}

func TestThrottlerQueuedCallerCanCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		throttler := ratelimit.NewThrottler(ratelimit.WithTierInterval(ratelimit.Tier4, time.Hour))
		if err := throttler.Wait(t.Context(), ratelimit.Tier4); err != nil {
			t.Fatal(err)
		}
		firstCtx, cancelFirst := context.WithCancel(t.Context())
		defer cancelFirst()
		first := make(chan error, 1)
		go func() { first <- throttler.Wait(firstCtx, ratelimit.Tier4) }()
		synctest.Wait()
		secondCtx, cancelSecond := context.WithCancel(t.Context())
		defer cancelSecond()
		second := make(chan error, 1)
		go func() { second <- throttler.Wait(secondCtx, ratelimit.Tier4) }()
		synctest.Wait()
		cancelSecond()
		synctest.Wait()
		select {
		case err := <-second:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("second caller error = %v", err)
			}
		default:
			t.Fatal("canceled caller remains blocked behind another caller")
		}
		cancelFirst()
		if err := <-first; !errors.Is(err, context.Canceled) {
			t.Fatalf("first caller error = %v", err)
		}
	})
}
