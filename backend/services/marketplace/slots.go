package main

import (
	"sort"
	"time"
)

// Bookable start times are on the half hour. The other booking rules are in
// settings.go.
const slotStep = 30 * time.Minute

type window struct {
	Weekday     int // time.Weekday, 0 = Sunday
	StartMinute int
	EndMinute   int
}

type interval struct{ Start, End time.Time }

func (a interval) overlaps(b interval) bool { return a.Start.Before(b.End) && b.Start.Before(a.End) }

// computeSlots lists the session start/end times a provider can be booked
// for between from and to. Windows are interpreted as wall-clock times in loc
// on each matching day, so a 09:00 window stays 09:00 across DST changes.
// Slots that overlap a busy interval are dropped.
func computeSlots(windows []window, loc *time.Location, from, to time.Time, dur time.Duration, busy []interval) []interval {
	sort.Slice(busy, func(i, j int) bool { return busy[i].Start.Before(busy[j].Start) })

	var out []interval
	localFrom := from.In(loc)
	day := time.Date(localFrom.Year(), localFrom.Month(), localFrom.Day(), 0, 0, 0, 0, loc)
	for ; day.Before(to); day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc) {
		for _, w := range windows {
			if time.Weekday(w.Weekday) != day.Weekday() {
				continue
			}
			winEnd := wallClock(day, w.EndMinute, loc)
			for m := w.StartMinute; ; m += int(slotStep / time.Minute) {
				start := wallClock(day, m, loc)
				end := start.Add(dur)
				if end.After(winEnd) {
					break
				}
				if start.Before(from) || end.After(to) {
					continue
				}
				slot := interval{start, end}
				if !isBusy(slot, busy) {
					out = append(out, slot)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return dedupe(out)
}

// wallClock returns the instant that minute m of day reads on a local clock.
func wallClock(day time.Time, m int, loc *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), m/60, m%60, 0, 0, loc)
}

func isBusy(slot interval, busy []interval) bool {
	for _, b := range busy {
		if b.Start.After(slot.End) {
			break
		}
		if slot.overlaps(b) {
			return true
		}
	}
	return false
}

// dedupe drops repeated starts from overlapping windows.
func dedupe(in []interval) []interval {
	out := in[:0]
	for i, s := range in {
		if i > 0 && s.Start.Equal(in[i-1].Start) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// validateWindows checks a full weekly schedule: bounds, half-hour alignment
// and no overlaps within a day.
func validateWindows(ws []window) error {
	if len(ws) > 70 {
		return errTooManyWindows
	}
	byDay := map[int][]window{}
	for _, w := range ws {
		if w.Weekday < 0 || w.Weekday > 6 || w.StartMinute < 0 || w.EndMinute > 1440 || w.EndMinute <= w.StartMinute {
			return errBadWindow
		}
		if w.StartMinute%30 != 0 || w.EndMinute%30 != 0 {
			return errUnalignedWindow
		}
		byDay[w.Weekday] = append(byDay[w.Weekday], w)
	}
	for _, day := range byDay {
		sort.Slice(day, func(i, j int) bool { return day[i].StartMinute < day[j].StartMinute })
		for i := 1; i < len(day); i++ {
			if day[i].StartMinute < day[i-1].EndMinute {
				return errOverlappingWindows
			}
		}
	}
	return nil
}
