package main

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestComputeSlotsBasic(t *testing.T) {
	loc := mustLoc(t, "Asia/Kolkata")
	// Monday 2026-10-12 08:00 IST.
	from := time.Date(2026, 10, 12, 8, 0, 0, 0, loc)
	to := from.Add(24 * time.Hour)
	ws := []window{{Weekday: int(time.Monday), StartMinute: 9 * 60, EndMinute: 11 * 60}}

	got := computeSlots(ws, loc, from, to, time.Hour, nil)
	want := []string{"09:00", "09:30", "10:00"}
	if len(got) != len(want) {
		t.Fatalf("got %d slots, want %d: %v", len(got), len(want), got)
	}
	for i, s := range got {
		if s.Start.In(loc).Format("15:04") != want[i] {
			t.Errorf("slot %d starts %s, want %s", i, s.Start.In(loc).Format("15:04"), want[i])
		}
		if s.End.Sub(s.Start) != time.Hour {
			t.Errorf("slot %d lasts %v", i, s.End.Sub(s.Start))
		}
	}
}

func TestComputeSlotsSkipsBusyAndPast(t *testing.T) {
	loc := mustLoc(t, "Europe/London")
	from := time.Date(2026, 10, 12, 9, 15, 0, 0, loc) // Monday, mid-window
	to := from.Add(12 * time.Hour)
	ws := []window{{Weekday: int(time.Monday), StartMinute: 9 * 60, EndMinute: 12 * 60}}
	busy := []interval{{time.Date(2026, 10, 12, 10, 30, 0, 0, loc), time.Date(2026, 10, 12, 11, 0, 0, 0, loc)}}

	got := computeSlots(ws, loc, from, to, 30*time.Minute, busy)
	var starts []string
	for _, s := range got {
		starts = append(starts, s.Start.In(loc).Format("15:04"))
	}
	want := []string{"09:30", "10:00", "11:00", "11:30"}
	if len(starts) != len(want) {
		t.Fatalf("got %v, want %v", starts, want)
	}
	for i := range want {
		if starts[i] != want[i] {
			t.Fatalf("got %v, want %v", starts, want)
		}
	}
}

func TestComputeSlotsKeepsWallClockAcrossDST(t *testing.T) {
	loc := mustLoc(t, "America/New_York")
	// DST ends Sunday 2026-11-01. A Saturday and a Monday 10:00 window
	// must both read 10:00 locally even though the UTC offset changed.
	from := time.Date(2026, 10, 31, 0, 0, 0, 0, loc)
	to := time.Date(2026, 11, 3, 0, 0, 0, 0, loc)
	ws := []window{
		{Weekday: int(time.Saturday), StartMinute: 600, EndMinute: 660},
		{Weekday: int(time.Monday), StartMinute: 600, EndMinute: 660},
	}
	got := computeSlots(ws, loc, from, to, time.Hour, nil)
	if len(got) != 2 {
		t.Fatalf("got %d slots, want 2", len(got))
	}
	for _, s := range got {
		if s.Start.In(loc).Format("15:04") != "10:00" {
			t.Errorf("slot reads %s locally, want 10:00", s.Start.In(loc).Format("15:04"))
		}
	}
	if got[0].Start.UTC().Hour() == got[1].Start.UTC().Hour() {
		t.Errorf("UTC hour should shift across DST, both are %d", got[0].Start.UTC().Hour())
	}
}

func TestValidateWindows(t *testing.T) {
	cases := []struct {
		name string
		ws   []window
		want error
	}{
		{"ok", []window{{1, 540, 600}, {1, 600, 660}, {2, 0, 1440}}, nil},
		{"overlap", []window{{1, 540, 660}, {1, 600, 720}}, errOverlappingWindows},
		{"unaligned", []window{{1, 545, 600}}, errUnalignedWindow},
		{"reversed", []window{{1, 600, 540}}, errBadWindow},
		{"bad day", []window{{7, 0, 60}}, errBadWindow},
	}
	for _, c := range cases {
		if got := validateWindows(c.ws); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
