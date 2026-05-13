package message

import (
	"errors"
	"testing"
	"time"
)

func TestParseScheduleWhen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr error
	}{
		{
			name:  "rfc3339 absolute",
			input: "2026-06-01T15:00:00-04:00",
			want:  time.Date(2026, 6, 1, 15, 0, 0, 0, time.FixedZone("", -4*60*60)),
		},
		{
			name:  "go duration",
			input: "2h30m",
			want:  now.Add(2*time.Hour + 30*time.Minute),
		},
		{
			name:  "bare unix seconds",
			input: "1780000000",
			want:  time.Unix(1780000000, 0).UTC(),
		},
		{
			name:    "past time",
			input:   "2026-05-13T13:59:59Z",
			wantErr: ErrScheduleInPast,
		},
		{
			name:    "beyond 120 days",
			input:   now.Add(maxScheduleAhead + time.Second).Format(time.RFC3339),
			wantErr: ErrScheduleBeyond120Days,
		},
		{
			name:    "natural language rejected",
			input:   "tomorrow",
			wantErr: ErrScheduleUnparseable,
		},
		{
			name:    "empty rejected",
			input:   "",
			wantErr: ErrScheduleUnparseable,
		},
		{
			name:    "garbage rejected",
			input:   "release-after-lunch",
			wantErr: ErrScheduleUnparseable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseScheduleWhen(tt.input, now)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("parseScheduleWhen(%q) error = %v, want %v", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseScheduleWhen(%q) returned error: %v", tt.input, err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("parseScheduleWhen(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseScheduleWhenRejectsUnsupportedFormats(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC)
	for _, input := range []string{
		"tomorrow at 9am",
		"next monday",
		"2026-06-01",
		"2026-06-01 15:00:00",
		"Mon, 01 Jun 2026 15:00:00 -0400",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			_, err := parseScheduleWhen(input, now)
			if !errors.Is(err, ErrScheduleUnparseable) {
				t.Fatalf("parseScheduleWhen(%q) error = %v, want %v", input, err, ErrScheduleUnparseable)
			}
		})
	}
}

func FuzzParseScheduleWhen(f *testing.F) {
	now := time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC)
	for _, seed := range []string{
		"2026-06-01T15:00:00Z",
		"90m",
		"1770000000",
		"tomorrow at 9am",
		"",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		got, err := parseScheduleWhen(input, now)
		if err == nil {
			if !got.After(now) || got.After(now.Add(maxScheduleAhead)) {
				t.Fatalf("parseScheduleWhen(%q) = %s outside valid schedule window", input, got)
			}
			return
		}
		if !errors.Is(err, ErrScheduleInPast) &&
			!errors.Is(err, ErrScheduleBeyond120Days) &&
			!errors.Is(err, ErrScheduleUnparseable) {
			t.Fatalf("parseScheduleWhen(%q) error = %v, want known sentinel", input, err)
		}
	})
}
