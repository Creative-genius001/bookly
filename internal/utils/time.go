package utils

import (
	errorMap "barber-booking-backend/internal/utils/error"
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
	errorMsg := fmt.Sprintf("invalid time %q; use HH:MM or h:mma", value)
	return "", errorMap.New(errorMap.CodeInvalidInput, "Time Utility: Normalize Clock", errorMsg)
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
		return time.Time{}, errorMap.New(errorMap.CodeInvalidInput, "Time Utility: Parse Date In Location", "date must use YYYY-MM-DD")
	}
	return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, loc), nil
}

func DateOnly(value time.Time, loc *time.Location) time.Time {
	local := value.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

func ValidateBookingWindow(date time.Time, loc *time.Location) error {
	now := time.Now()
	target := DateOnly(date, loc)
	today := DateOnly(now, loc)
	lastBookable := today.AddDate(0, 0, 14)

	if target.Before(today) {
		return errorMap.New(errorMap.CodeInvalidInput, "Time Utility: Validate Booking Window", "date cannot be in the past")
	}
	if target.After(lastBookable) {
		return errorMap.New(errorMap.CodeInvalidInput, "Time Utility: Validate Booking Window", "date must be within the rolling 14-day booking window")
	}
	return nil
}

func IsPastDate(dateStr string) (bool, error) {
	inputDate, err := time.Parse(DateLayout, dateStr)
	if err != nil {
		return false, errorMap.New(errorMap.CodeInvalidInput, "Time Utility: Is Past Date", "invalid date format")
	}

	now := time.Now().UTC()

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	return inputDate.Before(today), nil
}
