// Package money is the one and only money type in close-agent.
//
// INVARIANT (SPEC §1 golden rule, §4, §13.8): all money in this system is
// integer minor units — paise — carried as an int64. Floats NEVER touch money,
// anywhere. ₹2,360.00 is the value 236000. This package contains no float64 or
// float32, and a guard test asserts that statically.
//
// Why int64 paise: floating point cannot represent decimal fractions like 0.10
// exactly, so float arithmetic silently loses or invents paise and throws the
// trial balance out of balance. Integer paise is exact; ₹92,233,720,368,547.75
// fits in int64 with room to spare, far beyond any realistic period total.
//
// Money is a distinct named type (not a bare int64) so a raw count can't be
// passed where money is expected without an explicit conversion at the seam.
package money

import (
	"errors"
	"fmt"
	"strings"
)

// Money is an amount in paise (integer minor units). A positive value is a
// positive amount; a negative value is a negative amount. The ledger layers a
// debit/credit sign convention on top of this; Money itself is sign-agnostic
// and just stores an exact integer count of paise.
type Money int64

// paisePerRupee is the minor-unit scale for INR: 100 paise = 1 rupee. v1 is
// INR-only (SPEC §2), so this is a constant rather than per-currency config.
const paisePerRupee = 100

// FromPaise constructs Money directly from a count of paise. This is the
// canonical constructor — every other amount in the system ultimately flows
// through here, keeping "money is paise" the single representation.
func FromPaise(paise int64) Money { return Money(paise) }

// FromRupees constructs Money from a whole number of rupees. Convenience for
// fixtures and tests; it performs the ×100 scaling in integer space.
func FromRupees(rupees int64) Money { return Money(rupees * paisePerRupee) }

// Paise returns the underlying integer paise. This is the value to persist,
// serialize, or sum — never format it through a float.
func (m Money) Paise() int64 { return int64(m) }

// Add returns m + n. Both operands are paise, so the result is exact.
func (m Money) Add(n Money) Money { return m + n }

// Sub returns m - n.
func (m Money) Sub(n Money) Money { return m - n }

// Neg returns -m.
func (m Money) Neg() Money { return -m }

// IsZero reports whether the amount is exactly zero paise.
func (m Money) IsZero() bool { return m == 0 }

// Sign returns -1, 0, or +1 as m is negative, zero, or positive.
func (m Money) Sign() int {
	switch {
	case m < 0:
		return -1
	case m > 0:
		return 1
	default:
		return 0
	}
}

// String renders the amount as a plain rupee string with exactly two decimal
// places and no currency symbol or grouping, e.g. 236000 -> "2360.00",
// -5 -> "-0.05". It is the inverse of Parse, so Parse(m.String()) == m for
// every Money. Formatting is done entirely in integer/string space — no float.
func (m Money) String() string {
	neg := m < 0
	// Work on the magnitude to keep the decimal split simple; guard the int64
	// minimum, whose magnitude is not representable as a positive int64.
	var p int64
	if m == minMoney {
		// |MinInt64| overflows int64; format the known constant directly.
		return minMoneyString
	}
	p = int64(m)
	if neg {
		p = -p
	}

	rupees := p / paisePerRupee
	paise := p % paisePerRupee

	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	fmt.Fprintf(&b, "%d.%02d", rupees, paise)
	return b.String()
}

// Format is an alias for String, exposing the rupee rendering under the name
// the SPEC uses ("Format/String back to a rupee string").
func (m Money) Format() string { return m.String() }

const (
	// minMoney is the smallest representable amount; its magnitude cannot be
	// negated within int64, so String handles it as a special case.
	minMoney = Money(-1 << 63)
	// minMoneyString is the exact decimal rendering of minMoney in paise.
	// -9223372036854775808 paise = -92233720368547758.08 rupees.
	minMoneyString = "-92233720368547758.08"
)

// ErrParse is the sentinel wrapped by every Parse failure, so callers can test
// with errors.Is rather than string-matching.
var ErrParse = errors.New("money: invalid amount")

// Parse converts a rupee string such as "2360.00" or "-0.05" into Money
// (236000 / -5 paise). It is strict and float-free:
//
//   - an optional leading '-' or '+';
//   - one or more decimal digits for the rupee part;
//   - an optional '.' followed by exactly one or two paise digits.
//
// It rejects empty input, non-digit characters, more than two decimal places,
// a missing integer part, whitespace, and grouping separators. No float64 is
// used: the value is accumulated digit-by-digit in int64 paise.
func Parse(s string) (Money, error) {
	if s == "" {
		return 0, fmt.Errorf("%w: empty string", ErrParse)
	}

	neg := false
	body := s
	switch body[0] {
	case '-':
		neg = true
		body = body[1:]
	case '+':
		body = body[1:]
	}
	if body == "" {
		return 0, fmt.Errorf("%w: %q has a sign but no digits", ErrParse, s)
	}

	intPart := body
	fracPart := ""
	if dot := strings.IndexByte(body, '.'); dot >= 0 {
		intPart = body[:dot]
		fracPart = body[dot+1:]
		// Reject a second '.' lurking in the fractional remainder.
		if strings.IndexByte(fracPart, '.') >= 0 {
			return 0, fmt.Errorf("%w: %q has more than one decimal point", ErrParse, s)
		}
	}

	if intPart == "" {
		return 0, fmt.Errorf("%w: %q is missing the rupee part", ErrParse, s)
	}
	if len(fracPart) > 2 {
		return 0, fmt.Errorf("%w: %q has more than two decimal places (paise)", ErrParse, s)
	}

	rupees, err := parseDigits(intPart)
	if err != nil {
		return 0, fmt.Errorf("%w: %q rupee part: %v", ErrParse, s, err)
	}

	// Pad the fractional part to exactly two digits so "2360.5" reads as 50
	// paise, not 5. An empty fractional part (e.g. "2360" or "2360.") is 0.
	switch len(fracPart) {
	case 0:
		fracPart = "00"
	case 1:
		fracPart += "0"
	}
	paise, err := parseDigits(fracPart)
	if err != nil {
		return 0, fmt.Errorf("%w: %q paise part: %v", ErrParse, s, err)
	}

	total := rupees*paisePerRupee + paise
	if neg {
		total = -total
	}
	return Money(total), nil
}

// parseDigits accumulates a run of ASCII decimal digits into an int64 without
// using strconv's float-capable paths. An empty string, a non-digit byte, or
// overflow past int64 is an error.
func parseDigits(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("no digits")
	}
	var n int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("unexpected character %q", string(c))
		}
		d := int64(c - '0')
		// Detect overflow before it happens: n*10 + d must stay <= MaxInt64.
		const maxInt64 = int64(^uint64(0) >> 1)
		if n > (maxInt64-d)/10 {
			return 0, errors.New("value too large")
		}
		n = n*10 + d
	}
	return n, nil
}
