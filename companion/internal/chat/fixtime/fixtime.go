// Package fixtime moves the times in a recorded session to a made-up day, so a published fixture doesn't tell when
// it was recorded. A session carries its time in five ways, all moved by the same amount: ISO times
// (2026-01-01T12:00:00.000Z, and 2026-01-01T12-00-00 in file names), Unix times in seconds and in milliseconds,
// UUIDv7 ids (Codex, Pi: the first 48 bits are milliseconds), and OpenCode 2's ids (msg_, prt_: 12 hex digits of
// milliseconds×4096 plus a counter, mod 2^48; ses_: the same, inverted, so newer sorts first).
package fixtime

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"
)

// Day is where every recorded session starts.
var Day = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// Times outside this window are left alone: a number that only looks like a Unix time isn't one.
var lo, hi = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)

var (
	iso   = regexp.MustCompile(`\b(20\d\d-\d\d-\d\d)T(\d\d)([:-])(\d\d)([:-])(\d\d)(\.\d{1,9})?`)
	token = regexp.MustCompile(`[\w.]+`)                                                                                   // a Unix time is a whole token of 10 or 13 digits
	unix  = regexp.MustCompile(`^(1[78]\d{8}(?:\d{3})?)(\.\d+)?$`)                                                         // ms, or s with or without a fraction
	uuid7 = regexp.MustCompile(`(^|[^0-9a-f])([0-9a-f]{8})-([0-9a-f]{4})-(7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})\b`) // also in msg_<uuid>
	ocid  = regexp.MustCompile(`\b(msg|prt|ses)_([0-9a-f]{12})([0-9A-Za-z]{14})\b`)
)

const mask = 1<<48 - 1

func parseISO(m []string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02T15:04:05", m[1]+"T"+m[2]+":"+m[4]+":"+m[6])
	if err != nil {
		return time.Time{}, false
	}
	if m[7] != "" {
		f, _ := strconv.ParseFloat("0"+m[7], 64)
		t = t.Add(time.Duration(math.Round(f * 1e9)))
	}
	return t, t.After(lo) && t.Before(hi)
}

func parseUnix(s string) (time.Time, bool) {
	n, _ := strconv.ParseInt(s, 10, 64)
	t := time.Unix(n, 0)
	if len(s) == 13 {
		t = time.UnixMilli(n)
	}
	return t.UTC(), t.After(lo) && t.Before(hi)
}

func parseUUID(m []string) (time.Time, bool) {
	n, _ := strconv.ParseInt(m[2]+m[3], 16, 64)
	t := time.UnixMilli(n).UTC()
	return t, t.After(lo) && t.Before(hi)
}

// each calls f with every time b carries (OpenCode's ids aside: they hold the time only mod 2^36 ms).
func each(b []byte, f func(time.Time)) {
	s := string(b)
	for _, m := range iso.FindAllStringSubmatch(s, -1) {
		if t, ok := parseISO(m); ok {
			f(t)
		}
	}
	for _, w := range token.FindAllString(s, -1) {
		if m := unix.FindStringSubmatch(w); m != nil && (m[2] == "" || len(m[1]) == 10) {
			if t, ok := parseUnix(m[1]); ok {
				f(t)
			}
		}
	}
	for _, m := range uuid7.FindAllStringSubmatch(s, -1) {
		if t, ok := parseUUID(m); ok {
			f(t)
		}
	}
}

// Earliest is the first time b carries; false when it carries none.
func Earliest(b []byte) (time.Time, bool) {
	var first time.Time
	each(b, func(t time.Time) {
		if first.IsZero() || t.Before(first) {
			first = t
		}
	})
	return first, !first.IsZero()
}

// Latest is the last time b carries (zero when none).
func Latest(b []byte) time.Time {
	var last time.Time
	each(b, func(t time.Time) {
		if t.After(last) {
			last = t
		}
	})
	return last
}

// Shift moves every time in b so that the earliest falls on Day (to the second: whole seconds keep a Unix time in
// seconds and its twin in milliseconds in step). It returns b as is when b carries no time, and the amount moved.
func Shift(b []byte) ([]byte, time.Duration) {
	first, ok := Earliest(b)
	if !ok {
		return b, 0
	}
	d := Day.Sub(first.Truncate(time.Second))
	return By(b, d), d
}

// By moves every time in b by d, a whole number of seconds.
func By(b []byte, d time.Duration) []byte {
	if d%time.Second != 0 {
		panic("fixtime: a shift of a fraction of a second")
	}
	s := iso.ReplaceAllStringFunc(string(b), func(x string) string {
		m := iso.FindStringSubmatch(x)
		t, ok := parseISO(m)
		if !ok {
			return x
		}
		u := t.Add(d)
		out := u.Format("2006-01-02") + "T" + u.Format("15") + m[3] + u.Format("04") + m[5] + u.Format("05")
		if m[7] != "" {
			out += m[7] // a whole-second shift leaves the fraction as it was
		}
		return out
	})
	s = token.ReplaceAllStringFunc(s, func(x string) string {
		m := unix.FindStringSubmatch(x)
		if m == nil || (m[2] != "" && len(m[1]) != 10) {
			return x
		}
		t, ok := parseUnix(m[1])
		if !ok {
			return x
		}
		if len(m[1]) == 13 {
			return strconv.FormatInt(t.Add(d).UnixMilli(), 10)
		}
		return strconv.FormatInt(t.Add(d).Unix(), 10) + m[2] // a whole-second shift leaves the fraction as it was
	})
	s = uuid7.ReplaceAllStringFunc(s, func(x string) string {
		m := uuid7.FindStringSubmatch(x)
		t, ok := parseUUID(m)
		if !ok {
			return x
		}
		h := fmt.Sprintf("%012x", t.Add(d).UnixMilli())
		return m[1] + h[:8] + "-" + h[8:] + "-" + m[4]
	})
	s = ocid.ReplaceAllStringFunc(s, func(x string) string {
		m := ocid.FindStringSubmatch(x)
		n, _ := strconv.ParseUint(m[2], 16, 64)
		step := uint64(d.Milliseconds()) * 4096
		if m[1] == "ses" {
			n = (n - step) & mask
		} else {
			n = (n + step) & mask
		}
		return m[1] + "_" + fmt.Sprintf("%012x", n) + m[3]
	})
	return []byte(s)
}
