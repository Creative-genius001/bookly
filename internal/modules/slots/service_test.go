package slots

import (
	"testing"
	"time"
)

func TestBuildSlotTimesUsesConfiguredDuration(t *testing.T) {
	loc := time.UTC
	date := time.Date(2026, 5, 25, 0, 0, 0, 0, loc)

	got, err := BuildSlotTimes(date, "8:00am", "10:00am", 30, loc)
	if err != nil {
		t.Fatalf("BuildSlotTimes() error = %v", err)
	}

	if len(got) != 4 {
		t.Fatalf("expected 4 slots, got %d", len(got))
	}
	if got[0].Format("15:04") != "08:00" || got[3].Format("15:04") != "09:30" {
		t.Fatalf("unexpected slot boundaries: first=%s last=%s", got[0].Format("15:04"), got[3].Format("15:04"))
	}
}

func TestBuildSlotTimesDropsPartialTrailingSlot(t *testing.T) {
	loc := time.UTC
	date := time.Date(2026, 5, 25, 0, 0, 0, 0, loc)

	got, err := BuildSlotTimes(date, "8:00am", "9:20am", 45, loc)
	if err != nil {
		t.Fatalf("BuildSlotTimes() error = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected one complete slot, got %d", len(got))
	}
}
