package temporal

import (
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ParseInterval accepts String's canonical representation, ISO 8601 designator
// intervals and PostgreSQL's four output styles. Calendar components are whole
// numbers; only seconds may be fractional, to whole-microsecond precision.
// Input is bounded and components are checked without floating-point rounding.
func ParseInterval(text string) (Interval, error) {
	if len(text) == 0 || len(text) > 256 {
		return Interval{}, invalidIntervalText()
	}
	text = strings.TrimSpace(text)
	var parts intervalParts
	valid := false
	switch {
	case strings.HasPrefix(text, "P"):
		valid = parts.parseISO(text[1:])
	case strings.ContainsAny(text, "abcdefghijklmnopqrstuvwxyz@"):
		valid = parts.parseUnits(strings.Fields(text))
	default:
		valid = parts.parseSQL(strings.Fields(text))
	}
	if !valid || !parts.months.IsInt64() || !parts.days.IsInt64() || !parts.micros.IsInt64() {
		return Interval{}, invalidIntervalText()
	}
	months, days, micros := parts.months.Int64(), parts.days.Int64(), parts.micros.Int64()
	if months < math.MinInt32 || months > math.MaxInt32 || days < math.MinInt32 || days > math.MaxInt32 || micros < math.MinInt64/int64(time.Microsecond) || micros > math.MaxInt64/int64(time.Microsecond) {
		return Interval{}, invalidIntervalText()
	}
	return NewInterval(int32(months), int32(days), time.Duration(micros)*time.Microsecond)
}

func invalidIntervalText() error { return fault.New(fault.Invalid, "invalid or out-of-range interval") }

type intervalParts struct{ months, days, micros big.Int }
type intervalUnit struct {
	component, weight int64
	fractional        bool
}

func (p *intervalParts) add(text string, unit intervalUnit) bool {
	negative := strings.HasPrefix(text, "-")
	if negative || strings.HasPrefix(text, "+") {
		text = text[1:]
	}
	whole, fraction, decimal := strings.Cut(text, ".")
	if !intervalDigits(whole) || (decimal && (!unit.fractional || !intervalDigits(fraction) || len(fraction) > 6)) {
		return false
	}
	var n big.Int
	n.SetString(whole, 10)
	n.Mul(&n, big.NewInt(unit.weight))
	if decimal {
		var f big.Int
		f.SetString(fraction+strings.Repeat("0", 6-len(fraction)), 10)
		n.Add(&n, &f)
	}
	if negative {
		n.Neg(&n)
	}
	switch unit.component {
	case 0:
		p.months.Add(&p.months, &n)
	case 1:
		p.days.Add(&p.days, &n)
	case 2:
		p.micros.Add(&p.micros, &n)
	}
	return true
}
func intervalDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func (p *intervalParts) negate() {
	p.months.Neg(&p.months)
	p.days.Neg(&p.days)
	p.micros.Neg(&p.micros)
}

func intervalNamedUnit(name string) (intervalUnit, int, bool) {
	switch name {
	case "year", "years":
		return intervalUnit{0, 12, false}, 1, true
	case "mon", "mons", "month", "months":
		return intervalUnit{0, 1, false}, 2, true
	case "day", "days":
		return intervalUnit{1, 1, false}, 3, true
	case "hour", "hours":
		return intervalUnit{2, 3600000000, false}, 4, true
	case "min", "mins", "minute", "minutes":
		return intervalUnit{2, 60000000, false}, 5, true
	case "sec", "secs", "second", "seconds":
		return intervalUnit{2, 1000000, true}, 6, true
	case "microsecond", "microseconds":
		return intervalUnit{2, 1, false}, 7, true
	default:
		return intervalUnit{}, 0, false
	}
}

func (p *intervalParts) parseUnits(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	if tokens[0] == "@" {
		tokens = tokens[1:]
	}
	negate := len(tokens) > 0 && tokens[len(tokens)-1] == "ago"
	if negate {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) == 1 && tokens[0] == "0" {
		return true
	}
	if len(tokens) == 0 {
		return false
	}
	last := 0
	for len(tokens) > 0 {
		if strings.Contains(tokens[0], ":") {
			if len(tokens) != 1 || last >= 4 || !p.addClock(tokens[0]) {
				return false
			}
			tokens = tokens[1:]
			continue
		}
		if len(tokens) < 2 {
			return false
		}
		unit, order, ok := intervalNamedUnit(tokens[1])
		if !ok || order <= last || !p.add(tokens[0], unit) {
			return false
		}
		last = order
		tokens = tokens[2:]
	}
	if negate {
		p.negate()
	}
	return true
}

func (p *intervalParts) addClock(text string) bool {
	negative := strings.HasPrefix(text, "-")
	if negative || strings.HasPrefix(text, "+") {
		text = text[1:]
	}
	pieces := strings.Split(text, ":")
	if len(pieces) != 3 || !intervalDigits(pieces[0]) || len(pieces[1]) != 2 || !intervalDigits(pieces[1]) || pieces[1] > "59" {
		return false
	}
	seconds, _, _ := strings.Cut(pieces[2], ".")
	if len(seconds) != 2 || !intervalDigits(seconds) || seconds > "59" {
		return false
	}
	var clock intervalParts
	if !clock.add(pieces[0], intervalUnit{2, 3600000000, false}) || !clock.add(pieces[1], intervalUnit{2, 60000000, false}) || !clock.add(pieces[2], intervalUnit{2, 1000000, true}) {
		return false
	}
	if negative {
		clock.negate()
	}
	p.micros.Add(&p.micros, &clock.micros)
	return true
}

func (p *intervalParts) parseISO(text string) bool {
	if text == "" {
		return false
	}
	clock, seen, last := false, false, 0
	for text != "" {
		if text[0] == 'T' {
			if clock || len(text) == 1 {
				return false
			}
			clock = true
			text = text[1:]
			continue
		}
		i := 0
		for i < len(text) && ((text[i] >= '0' && text[i] <= '9') || text[i] == '-' || text[i] == '+' || text[i] == '.') {
			i++
		}
		if i == 0 || i == len(text) {
			return false
		}
		number, designator := text[:i], text[i]
		unit, order := intervalUnit{}, 0
		if clock {
			switch designator {
			case 'H':
				unit, order = intervalUnit{2, 3600000000, false}, 5
			case 'M':
				unit, order = intervalUnit{2, 60000000, false}, 6
			case 'S':
				unit, order = intervalUnit{2, 1000000, true}, 7
			default:
				return false
			}
		} else {
			switch designator {
			case 'Y':
				unit, order = intervalUnit{0, 12, false}, 1
			case 'M':
				unit, order = intervalUnit{0, 1, false}, 2
			case 'W':
				unit, order = intervalUnit{1, 7, false}, 3
			case 'D':
				unit, order = intervalUnit{1, 1, false}, 4
			default:
				return false
			}
		}
		if order <= last || !p.add(number, unit) {
			return false
		}
		seen, last = true, order
		text = text[i+1:]
	}
	return seen
}

func (p *intervalParts) parseSQL(tokens []string) bool {
	if len(tokens) == 0 || len(tokens) > 3 {
		return false
	}
	if len(tokens) == 1 && tokens[0] == "0" {
		return true
	}
	// A single leading minus applies to all components in SQL-standard output.
	// Mixed signs are explicit on later fields and disable that inheritance.
	negative := strings.HasPrefix(tokens[0], "-")
	for _, token := range tokens[1:] {
		if strings.HasPrefix(token, "+") || strings.HasPrefix(token, "-") {
			negative = false
		}
	}
	if negative {
		tokens = append([]string(nil), tokens...)
		tokens[0] = tokens[0][1:]
	}
	first := tokens[0]
	sign := ""
	if strings.HasPrefix(first, "+") || strings.HasPrefix(first, "-") {
		sign, first = first[:1], first[1:]
	}
	if years, months, ok := strings.Cut(first, "-"); ok {
		if !intervalDigits(years) || !intervalDigits(months) || len(months) > 2 || !p.add(sign+years, intervalUnit{0, 12, false}) || !p.add(sign+months, intervalUnit{0, 1, false}) {
			return false
		}
		var n big.Int
		n.SetString(months, 10)
		if n.Int64() > 11 {
			return false
		}
		tokens = tokens[1:]
	}
	if len(tokens) > 0 && !strings.Contains(tokens[0], ":") {
		if len(tokens) != 2 || !p.add(tokens[0], intervalUnit{1, 1, false}) {
			return false
		}
		tokens = tokens[1:]
	}
	if len(tokens) > 0 {
		if len(tokens) != 1 || !p.addClock(tokens[0]) {
			return false
		}
	}
	if negative {
		p.negate()
	}
	return true
}
