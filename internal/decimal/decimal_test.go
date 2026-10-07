package decimal

import (
	"encoding/json"
	"testing"
)

func TestParseAndString(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"0", "0.00"},
		{"1", "1.00"},
		{"1.5", "1.50"},
		{"-1.5", "-1.50"},
		{"1234.567", "1234.567"},
		{"0.1800", "0.18"},
		{"10.12340000", "10.1234"},
	}
	// Non-zero digits beyond the supported scale are rejected, not rounded.
	if _, err := Parse("10.12345"); err == nil {
		t.Fatalf("expected error for 5 fractional digits")
	}
	for _, c := range cases {
		d, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.in, err)
		}
		if got := d.String(); got != c.out {
			t.Errorf("Parse(%q).String() = %q, want %q", c.in, got, c.out)
		}
	}
	for _, bad := range []string{"", "-", "1.2.3", "1e5", "1,000", "abc"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestArithmetic(t *testing.T) {
	a := MustParse("1000.00")
	rate := MustParse("0.18")
	if got := a.Mul(rate).String(); got != "180.00" {
		t.Errorf("mul = %s", got)
	}
	// 33.33 * 0.18 = 5.9994 -> rounds to 6.00 at 2 places
	if got := MustParse("33.33").Mul(rate).StringFixed(2); got != "6.00" {
		t.Errorf("round = %s", got)
	}
	if got := MustParse("-2.345").Round(2).String(); got != "-2.35" {
		t.Errorf("neg round = %s", got)
	}
	q, err := MustParse("118").Div(MustParse("1.18"))
	if err != nil || q.String() != "100.00" {
		t.Errorf("div = %s, %v", q, err)
	}
	if MustParse("12.30").Places() != 1 || MustParse("12").Places() != 0 {
		t.Errorf("places")
	}
	if MustParse("123456.7").IntDigits() != 6 {
		t.Errorf("intdigits")
	}
}

func TestJSON(t *testing.T) {
	var v struct {
		A Decimal `json:"a"`
		B Decimal `json:"b"`
	}
	if err := json.Unmarshal([]byte(`{"a": 12.345, "b": "7.10"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.A.String() != "12.345" || v.B.String() != "7.10" {
		t.Fatalf("got %s %s", v.A, v.B)
	}
	out, _ := json.Marshal(v)
	if string(out) != `{"a":12.345,"b":7.10}` {
		t.Fatalf("marshal: %s", out)
	}
}
