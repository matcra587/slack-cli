package message

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	ErrScheduleInPast        = errors.New("schedule time must be in the future")
	ErrScheduleBeyond120Days = errors.New("schedule time must be within 120 days")
	ErrScheduleUnparseable   = errors.New("schedule time must be RFC3339, Go duration, or Unix seconds")
)

const maxScheduleAhead = 120 * 24 * time.Hour

func parseScheduleWhen(input string, now time.Time) (time.Time, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return time.Time{}, ErrScheduleUnparseable
	}

	postAt, err := parseScheduleValue(value, now)
	if err != nil {
		return time.Time{}, err
	}
	if !postAt.After(now) {
		return time.Time{}, ErrScheduleInPast
	}
	if postAt.After(now.Add(maxScheduleAhead)) {
		return time.Time{}, ErrScheduleBeyond120Days
	}
	return postAt, nil
}

func parseScheduleValue(value string, now time.Time) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(unix, 0).UTC(), nil
	}
	if duration, err := time.ParseDuration(value); err == nil {
		return now.Add(duration), nil
	}
	return time.Time{}, ErrScheduleUnparseable
}
