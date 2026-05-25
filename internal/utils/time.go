package utils

import (
	"fmt"
	"strings"
	"time"
)

const DateLayout = "2006-01-02"

var clockLayouts = []string{
	"15:04",
	"3:04PM",
	"3PM",
	"03:04PM",
	"03PM",
}

func NormalizeClock(value string) (string, error) {
	clean := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
	for _, layout := range clockLayouts {
		parsed, err := time.Parse(layout, clean)
		if err == nil {
			return parsed.Format("15:04"), nil
		}
	}
	return "", fmt.Errorf("invalid time %q; use HH:MM or h:mma", value)
}

func ClockOnDate(date time.Time, clock string, loc *time.Location) (time.Time, error) {
	normalized, err := NormalizeClock(clock)
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.ParseInLocation("15:04", normalized, loc)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(date.Year(), date.Month(), date.Day(), parsed.Hour(), parsed.Minute(), 0, 0, loc), nil
}

func ParseDateInLocation(value string, loc *time.Location) (time.Time, error) {
	parsed, err := time.ParseInLocation(DateLayout, value, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("date must use YYYY-MM-DD")
	}
	return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, loc), nil
}

func DateOnly(value time.Time, loc *time.Location) time.Time {
	local := value.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

func ValidateBookingWindow(date time.Time, loc *time.Location, now time.Time) error {
	target := DateOnly(date, loc)
	today := DateOnly(now, loc)
	lastBookable := today.AddDate(0, 0, 14)

	if target.Before(today) {
		return fmt.Errorf("date cannot be in the past")
	}
	if target.After(lastBookable) {
		return fmt.Errorf("date must be within the rolling 14-day booking window")
	}
	return nil
}
