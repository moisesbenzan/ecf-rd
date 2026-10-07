// Package decimal implements a small, allocation-free fixed-point decimal type
// suitable for monetary amounts in e-CF documents.
//
// Values are stored as an int64 scaled by 10^4, which covers every precision
// used by the DGII formats (amounts use 2 decimals, unit prices and exchange
// rates up to 4) while keeping arithmetic exact and fast. The supported range
// is roughly ±922,337,203,685.4775, far above any realistic invoice amount;
// operations that overflow return an error instead of wrapping silently.
package decimal

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strconv"
)

// Scale is the number of fractional digits stored internally.
const Scale = 4

const factor = 10000 // 10^Scale

var pow10 = [...]int64{1, 10, 100, 1000, 10000}

// ErrOverflow is returned when a value does not fit in the internal range.
var ErrOverflow = errors.New("decimal: overflow")

// ErrSyntax is returned when parsing an invalid decimal string.
var ErrSyntax = errors.New("decimal: invalid syntax")

// Decimal is a fixed-point number with 4 fractional digits.
// The zero value is 0.
type Decimal struct {
	v int64
}

// Zero is the zero value.
var Zero = Decimal{}

// New returns value * 10^-exp. exp must be between 0 and 4.
func New(value int64, exp int) Decimal {
	if exp < 0 || exp > Scale {
		panic("decimal: exponent out of range")
	}
	return Decimal{v: value * pow10[Scale-exp]}
}

// FromInt returns the decimal representation of i.
func FromInt(i int64) Decimal { return Decimal{v: i * factor} }

// MustParse is like Parse but panics on error. Intended for constants/tests.
func MustParse(s string) Decimal {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// Parse parses a plain decimal string such as "-1234.56". Exponents and
// thousands separators are rejected. More than 4 fractional digits is an
// error rather than a silent rounding.
func Parse(s string) (Decimal, error) {
	if s == "" {
		return Zero, ErrSyntax
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	if s == "" {
		return Zero, ErrSyntax
	}
	var intPart, fracPart int64
	fracDigits := 0
	seenDot := false
	digits := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '.':
			if seenDot {
				return Zero, ErrSyntax
			}
			seenDot = true
		case c >= '0' && c <= '9':
			digits++
			if seenDot {
				if fracDigits == Scale {
					// Allow trailing zeros beyond the supported scale.
					if c != '0' {
						return Zero, fmt.Errorf("decimal: more than %d fractional digits in %q", Scale, s)
					}
					continue
				}
				fracPart = fracPart*10 + int64(c-'0')
				fracDigits++
			} else {
				if intPart > (math.MaxInt64/factor-9)/10 {
					return Zero, ErrOverflow
				}
				intPart = intPart*10 + int64(c-'0')
			}
		default:
			return Zero, ErrSyntax
		}
	}
	if digits == 0 {
		return Zero, ErrSyntax
	}
	v := intPart*factor + fracPart*pow10[Scale-fracDigits]
	if neg {
		v = -v
	}
	return Decimal{v: v}, nil
}

// Raw returns the internal scaled representation (value * 10^4).
func (d Decimal) Raw() int64 { return d.v }

// IsZero reports whether d == 0.
func (d Decimal) IsZero() bool { return d.v == 0 }

// Sign returns -1, 0 or 1.
func (d Decimal) Sign() int {
	switch {
	case d.v < 0:
		return -1
	case d.v > 0:
		return 1
	}
	return 0
}

// Cmp compares d and o, returning -1, 0 or 1.
func (d Decimal) Cmp(o Decimal) int {
	switch {
	case d.v < o.v:
		return -1
	case d.v > o.v:
		return 1
	}
	return 0
}

// Equal reports whether d == o.
func (d Decimal) Equal(o Decimal) bool { return d.v == o.v }

// Neg returns -d.
func (d Decimal) Neg() Decimal { return Decimal{v: -d.v} }

// Abs returns |d|.
func (d Decimal) Abs() Decimal {
	if d.v < 0 {
		return Decimal{v: -d.v}
	}
	return d
}

// Add returns d + o.
func (d Decimal) Add(o Decimal) Decimal { return Decimal{v: d.v + o.v} }

// Sub returns d - o.
func (d Decimal) Sub(o Decimal) Decimal { return Decimal{v: d.v - o.v} }

// Mul returns d * o rounded half away from zero to 4 fractional digits.
// It panics on overflow; use MulChecked when inputs are untrusted.
func (d Decimal) Mul(o Decimal) Decimal {
	r, err := d.MulChecked(o)
	if err != nil {
		panic(err)
	}
	return r
}

// MulChecked returns d * o rounded half away from zero to 4 fractional digits.
func (d Decimal) MulChecked(o Decimal) (Decimal, error) {
	neg := (d.v < 0) != (o.v < 0)
	a, b := uabs(d.v), uabs(o.v)
	hi, lo := bits.Mul64(a, b)
	if hi >= factor {
		return Zero, ErrOverflow
	}
	q, rem := bits.Div64(hi, lo, factor)
	if rem*2 >= factor {
		q++
	}
	if q > math.MaxInt64 {
		return Zero, ErrOverflow
	}
	v := int64(q)
	if neg {
		v = -v
	}
	return Decimal{v: v}, nil
}

// Div returns d / o rounded half away from zero to 4 fractional digits.
func (d Decimal) Div(o Decimal) (Decimal, error) {
	if o.v == 0 {
		return Zero, errors.New("decimal: division by zero")
	}
	neg := (d.v < 0) != (o.v < 0)
	a, b := uabs(d.v), uabs(o.v)
	hi, lo := bits.Mul64(a, factor)
	if hi >= b {
		return Zero, ErrOverflow
	}
	q, rem := bits.Div64(hi, lo, b)
	if rem >= b-rem { // rem*2 >= b without overflow
		q++
	}
	if q > math.MaxInt64 {
		return Zero, ErrOverflow
	}
	v := int64(q)
	if neg {
		v = -v
	}
	return Decimal{v: v}, nil
}

// Round rounds d half away from zero to the given number of fractional
// digits (0-4).
func (d Decimal) Round(places int) Decimal {
	if places >= Scale {
		return d
	}
	if places < 0 {
		places = 0
	}
	unit := pow10[Scale-places]
	q := d.v / unit
	r := d.v % unit
	if r < 0 {
		r = -r
	}
	if r*2 >= unit {
		if d.v < 0 {
			q--
		} else {
			q++
		}
	}
	return Decimal{v: q * unit}
}

// Places returns the number of significant fractional digits (0-4).
func (d Decimal) Places() int {
	v := d.v
	if v < 0 {
		v = -v
	}
	p := Scale
	for p > 0 && v%10 == 0 {
		v /= 10
		p--
	}
	return p
}

// IntDigits returns the number of digits in the integer part.
func (d Decimal) IntDigits() int {
	i := uabs(d.v) / factor
	n := 1
	for i >= 10 {
		i /= 10
		n++
	}
	return n
}

// StringFixed formats d with exactly the given number of fractional digits,
// rounding half away from zero when necessary.
func (d Decimal) StringFixed(places int) string {
	if places > Scale {
		places = Scale
	}
	if places < 0 {
		places = 0
	}
	r := d.Round(places)
	var buf [32]byte
	return string(r.appendFixed(buf[:0], places))
}

// String formats d with at least 2 and at most 4 fractional digits, trimming
// trailing zeros beyond the second decimal. This is the canonical
// representation used in DGII XML documents ("100.00", "1.1800", "0.5").
func (d Decimal) String() string {
	places := d.Places()
	if places < 2 {
		places = 2
	}
	var buf [32]byte
	return string(d.appendFixed(buf[:0], places))
}

func (d Decimal) appendFixed(b []byte, places int) []byte {
	v := d.v
	if v < 0 {
		b = append(b, '-')
	}
	u := uabs(v)
	b = strconv.AppendUint(b, u/factor, 10)
	if places == 0 {
		return b
	}
	frac := (u % factor) / uint64(pow10[Scale-places])
	b = append(b, '.')
	s := strconv.FormatUint(frac, 10)
	for i := len(s); i < places; i++ {
		b = append(b, '0')
	}
	return append(b, s...)
}

// Int64 returns the integer part of d (truncated toward zero).
func (d Decimal) Int64() int64 { return d.v / factor }

func uabs(v int64) uint64 {
	if v < 0 {
		return uint64(-v)
	}
	return uint64(v)
}
