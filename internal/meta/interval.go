package meta

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// interval holds an interval as PostgreSQL keeps one, in parts that do not
// convert into each other: months, days, and the time in nanoseconds.
type interval struct {
	months total
	days   total
	nanos  total
}

var (
	errDuration = errors.New("does not fit a time.Duration")
	errSyntax   = errors.New("is not an interval")
)

// duration counts the interval as PostgreSQL's EXTRACT(EPOCH FROM ...) does: a
// whole year is 365.25 days, a month left over is 30 days, and a day is 24
// hours.
func (i interval) duration() (time.Duration, error) {
	// Four times the days, so that a year's quarter of a day is whole.
	var quarters, seconds, out total

	quarters.addTimes(1461, i.months.v/12)
	quarters.addTimes(120, i.months.v%12)
	quarters.addTimes(4, i.days.v)
	seconds.addTimes(6*60*60, quarters.v)
	out.addTimes(int64(time.Second), seconds.v)
	out.add(i.nanos.v)

	for _, t := range []total{i.months, i.days, i.nanos, quarters, seconds, out} {
		if t.over {
			return 0, errDuration
		}
	}

	return time.Duration(out.v), nil
}

// total is a sum of int64s that remembers whether it overflowed.
type total struct {
	v    int64
	over bool
}

func (t *total) add(n int64) {
	next := t.v + n
	if n > 0 && next < t.v || n < 0 && next > t.v {
		t.over = true
	}

	t.v = next
}

func (t *total) addTimes(a, b int64) {
	if a == 0 || b == 0 {
		return
	}

	p := a * b
	if p/b != a || a == -1 && b == math.MinInt64 || b == -1 && a == math.MinInt64 {
		t.over = true

		return
	}

	t.add(p)
}

func (i *interval) negate() {
	for _, t := range []*total{&i.months, &i.days, &i.nanos} {
		if t.v == math.MinInt64 {
			t.over = true
		}

		t.v = -t.v
	}
}

// ParseDuration reads the text of an interval, as parseInterval does, as a
// time.Duration.
func ParseDuration(text string) (time.Duration, error) {
	i, err := parseInterval(text)
	if err != nil {
		return 0, err
	}

	out, err := i.duration()
	if err != nil {
		return 0, fmt.Errorf("ant: %q %w", text, err)
	}

	return out, nil
}

// parseInterval reads an interval as PostgreSQL writes one in each of its
// IntervalStyle settings:
//
//	postgres          1 year 2 mons -3 days +04:05:06.5
//	postgres_verbose  @ 1 year 2 mons -3 days 4 hours 5 mins 6.5 secs ago
//	sql_standard      +1-2 -3 +4:05:06.5
//	iso_8601          P1Y2M-3DT4H5M6.5S
//
// A time alone, 04:05:06, is the same in all but iso_8601, and is how MySQL
// writes a TIME. An infinite interval has no duration, so it is an error.
func parseInterval(text string) (interval, error) {
	s := strings.TrimSpace(text)

	var (
		out interval
		err error
	)

	switch {
	case s == "infinity" || s == "-infinity":
		err = errDuration
	case strings.HasPrefix(s, "P"):
		out, err = parseISOInterval(s[1:])
	default:
		out, err = parseWordInterval(s)
	}

	if err != nil {
		return interval{}, fmt.Errorf("ant: %q %w", text, err)
	}

	return out, nil
}

// parseWordInterval reads the postgres, postgres_verbose and sql_standard
// forms, which are fields apart by spaces.
func parseWordInterval(s string) (interval, error) {
	var out interval

	fields := strings.Fields(s)

	if len(fields) > 0 && fields[0] == "@" {
		fields = fields[1:]
	}

	// postgres_verbose writes the fields as they are after the first one's sign,
	// and ago if that was a minus.
	negative := len(fields) > 0 && fields[len(fields)-1] == "ago"
	if negative {
		fields = fields[:len(fields)-1]
	}

	if len(fields) == 0 {
		return out, errSyntax
	}

	// sql_standard writes one minus before the first field when every field is
	// negative, and a sign before each field when they differ. The other forms
	// name each field's unit.
	if !anyUnit(fields) && len(fields) > 1 && fields[0][0] == '-' && !anySign(fields[1:]) {
		negative = !negative
		fields[0] = fields[0][1:]
	}

	for i := 0; i < len(fields); i++ {
		field := fields[i]

		switch {
		case strings.Contains(field, ":"):
			nanos, err := parseClock(field)
			if err != nil {
				return out, err
			}

			out.nanos.add(nanos)
		case i+1 < len(fields) && unitOf(fields[i+1]) != "":
			unit := unitOf(fields[i+1])
			i++

			if unit == "sec" {
				nanos, err := parseSeconds(field)
				if err != nil {
					return out, err
				}

				out.nanos.add(nanos)

				continue
			}

			n, err := parseWhole(field)
			if err != nil {
				return out, err
			}

			switch unit {
			case "year":
				out.months.addTimes(12, n)
			case "mon":
				out.months.add(n)
			case "day":
				out.days.add(n)
			case "hour":
				out.nanos.addTimes(int64(time.Hour), n)
			case "min":
				out.nanos.addTimes(int64(time.Minute), n)
			}
		case isYearMonth(field):
			sign := int64(1)

			if field[0] == '-' {
				sign = -1
			}

			y, m, _ := strings.Cut(strings.TrimLeft(field, "+-"), "-")

			years, err := parseWhole(y)
			if err != nil {
				return out, err
			}

			months, err := parseWhole(m)
			if err != nil {
				return out, err
			}

			out.months.addTimes(12*sign, years)
			out.months.addTimes(sign, months)
		default:
			// A number alone is days in sql_standard, and zero there and in
			// postgres_verbose.
			n, err := parseWhole(field)
			if err != nil {
				return out, err
			}

			out.days.add(n)
		}
	}

	if negative {
		out.negate()
	}

	return out, nil
}

// unitOf returns the unit a postgres or postgres_verbose field names, or "".
func unitOf(word string) string {
	switch word {
	case "year", "years":
		return "year"
	case "mon", "mons":
		return "mon"
	case "day", "days":
		return "day"
	case "hour", "hours":
		return "hour"
	case "min", "mins":
		return "min"
	case "sec", "secs":
		return "sec"
	}

	return ""
}

func anyUnit(fields []string) bool {
	for _, f := range fields {
		if unitOf(f) != "" {
			return true
		}
	}

	return false
}

func anySign(fields []string) bool {
	for _, f := range fields {
		if f[0] == '-' || f[0] == '+' {
			return true
		}
	}

	return false
}

// isYearMonth reports whether a field is sql_standard's years and months, such
// as 1-2 or -1-2.
func isYearMonth(field string) bool {
	rest := field

	if rest[0] == '-' || rest[0] == '+' {
		rest = rest[1:]
	}

	y, m, ok := strings.Cut(rest, "-")

	return ok && digits(y) && digits(m)
}

func digits(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// parseWhole reads a whole number with an optional sign.
func parseWhole(field string) (int64, error) {
	if !digits(strings.TrimPrefix(strings.TrimPrefix(field, "-"), "+")) {
		return 0, errSyntax
	}

	n, err := strconv.ParseInt(field, 10, 64)
	if err != nil {
		return 0, errDuration
	}

	return n, nil
}

// parseClock reads hours, minutes and seconds with a sign, such as -04:05:06.5.
func parseClock(field string) (int64, error) {
	sign := int64(1)

	switch field[0] {
	case '-':
		sign = -1

		field = field[1:]
	case '+':
		field = field[1:]
	}

	parts := strings.Split(field, ":")
	if len(parts) < 2 || len(parts) > 3 || !digits(parts[0]) || len(parts[1]) != 2 ||
		!digits(parts[1]) {
		return 0, errSyntax
	}

	hours, err := parseWhole(parts[0])
	if err != nil {
		return 0, err
	}

	minutes, _ := strconv.ParseInt(parts[1], 10, 64)

	var seconds int64

	if len(parts) == 3 {
		whole, _, _ := strings.Cut(parts[2], ".")
		if len(whole) != 2 {
			return 0, errSyntax
		}

		seconds, err = parseSeconds(parts[2])
		if err != nil {
			return 0, err
		}
	}

	if minutes > 59 || seconds >= int64(time.Minute) {
		return 0, errSyntax
	}

	var out total

	out.addTimes(int64(time.Hour), hours)
	out.addTimes(int64(time.Minute), minutes)
	out.add(seconds)

	if out.over {
		return 0, errDuration
	}

	return sign * out.v, nil
}

// parseSeconds reads seconds with up to nine places and a sign, as
// nanoseconds.
func parseSeconds(field string) (int64, error) {
	sign := int64(1)

	switch {
	case strings.HasPrefix(field, "-"):
		sign = -1

		field = field[1:]
	case strings.HasPrefix(field, "+"):
		field = field[1:]
	}

	whole, fraction, dotted := strings.Cut(field, ".")
	if !digits(whole) || dotted && (!digits(fraction) || len(fraction) > 9) {
		return 0, errSyntax
	}

	seconds, err := parseWhole(whole)
	if err != nil {
		return 0, err
	}

	var out total

	out.addTimes(int64(time.Second), seconds)

	if dotted {
		nanos, _ := strconv.ParseInt(fraction+strings.Repeat("0", 9-len(fraction)), 10, 64)
		out.add(nanos)
	}

	if out.over {
		return 0, errDuration
	}

	return sign * out.v, nil
}

// parseISOInterval reads iso_8601's form after its P: years, months, weeks and
// days, then a T and hours, minutes and seconds, each a number before its
// letter, any of them negative.
func parseISOInterval(s string) (interval, error) {
	var (
		out   interval
		clock bool
		parts int
	)

	for s != "" {
		if s[0] == 'T' && !clock {
			clock = true
			s = s[1:]

			continue
		}

		end := strings.IndexAny(s, "YMWDHS")
		if end <= 0 {
			return out, errSyntax
		}

		number, letter := s[:end], s[end]
		s = s[end+1:]
		parts++

		if clock && letter == 'S' {
			nanos, err := parseSeconds(number)
			if err != nil {
				return out, err
			}

			out.nanos.add(nanos)

			continue
		}

		n, err := parseWhole(number)
		if err != nil {
			return out, err
		}

		switch {
		case !clock && letter == 'Y':
			out.months.addTimes(12, n)
		case !clock && letter == 'M':
			out.months.add(n)
		case !clock && letter == 'W':
			out.days.addTimes(7, n)
		case !clock && letter == 'D':
			out.days.add(n)
		case clock && letter == 'H':
			out.nanos.addTimes(int64(time.Hour), n)
		case clock && letter == 'M':
			out.nanos.addTimes(int64(time.Minute), n)
		default:
			return out, errSyntax
		}
	}

	if parts == 0 {
		return out, errSyntax
	}

	return out, nil
}
