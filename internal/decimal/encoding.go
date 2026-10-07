package decimal

import (
	"bytes"
	"strconv"
)

// MarshalText implements encoding.TextMarshaler, used by encoding/xml.
func (d Decimal) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Decimal) UnmarshalText(b []byte) error {
	v, err := Parse(string(bytes.TrimSpace(b)))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// MarshalJSON encodes d as a JSON number, preserving the exact value.
func (d Decimal) MarshalJSON() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalJSON accepts a JSON number or a JSON string containing a number.
// Numbers are parsed from their literal text, never through float64.
func (d *Decimal) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		s, err := strconv.Unquote(string(b))
		if err != nil {
			return err
		}
		return d.UnmarshalText([]byte(s))
	}
	return d.UnmarshalText(b)
}
