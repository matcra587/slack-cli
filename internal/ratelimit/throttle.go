package ratelimit

import (
	"context"
	"time"
)

type Clock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type Throttler struct {
	clock     Clock
	disabled  bool
	intervals map[Tier]time.Duration
	last      map[Tier]time.Time
}

type ThrottlerOption func(*Throttler)

func NewThrottler(opts ...ThrottlerOption) *Throttler {
	t := &Throttler{
		clock: realClock{},
		intervals: map[Tier]time.Duration{
			Tier1:       time.Minute,
			Tier2:       3 * time.Second,
			Tier3:       1200 * time.Millisecond,
			Tier4:       600 * time.Millisecond,
			TierSpecial: time.Second,
			TierUnknown: time.Second,
		},
		last: map[Tier]time.Time{},
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func WithClock(clock Clock) ThrottlerOption {
	return func(t *Throttler) {
		if clock != nil {
			t.clock = clock
		}
	}
}

func WithDisabled(disabled bool) ThrottlerOption {
	return func(t *Throttler) {
		t.disabled = disabled
	}
}

func WithTierInterval(tier Tier, interval time.Duration) ThrottlerOption {
	return func(t *Throttler) {
		t.intervals[tier] = interval
	}
}

func (t *Throttler) Wait(ctx context.Context, tier Tier) error {
	if t == nil || t.disabled {
		return nil
	}

	now := t.clock.Now()
	interval := t.intervals[tier]
	if interval <= 0 {
		t.last[tier] = now
		return nil
	}

	if last, ok := t.last[tier]; ok {
		if remaining := interval - now.Sub(last); remaining > 0 {
			if err := t.clock.Sleep(ctx, remaining); err != nil {
				return err
			}
			now = t.clock.Now()
		}
	}

	t.last[tier] = now
	return nil
}
