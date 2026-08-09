package money

import (
	"encoding/json"
	"testing"
)

func TestParseDecimal(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    Amount
		wantErr bool
	}{
		{name: "one decimal place", in: "18.5", want: 1850},
		{name: "two decimal places", in: "18.50", want: 1850},
		{name: "integer", in: "12", want: 1200},
		{name: "zero", in: "0", want: 0},
		{name: "half rounds away from zero", in: "0.005", want: 1},
		{name: "below halfway rounds down", in: "0.004", want: 0},
		{name: "negative half is symmetric", in: "-0.005", want: -1},
		{name: "carry into next cent", in: "1.005", want: 101},
		{name: "float64 trap 2.675*100 == 267.49999999999997", in: "2.675", want: 268},
		{name: "empty", in: "", wantErr: true},
		{name: "blank", in: "  ", wantErr: true},
		{name: "letters", in: "abc", wantErr: true},
		{name: "two separators", in: "1.2.3", wantErr: true},
		{name: "currency prefix", in: "RM18.50", wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseDecimal(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ParseDecimal(%q) = %d, want error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDecimal(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("ParseDecimal(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestApplyDiscount(t *testing.T) {
	cases := []struct {
		name   string
		amount Amount
		bps    int64
		want   Amount
	}{
		{name: "10% off 185.00", amount: 18500, bps: 1000, want: 16650},
		{name: "no discount passes through", amount: 1200, bps: 0, want: 1200},
		{name: "100% off", amount: 10000, bps: 10000, want: 0},
		{name: "333 * 0.6667 = 222.0111", amount: 333, bps: 3333, want: 222},
		{name: "half cent rounds away from zero", amount: 1, bps: 5000, want: 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.amount.ApplyDiscount(c.bps); got != c.want {
				t.Errorf("Amount(%d).ApplyDiscount(%d) = %d, want %d", c.amount, c.bps, got, c.want)
			}
		})
	}
}

// ERP B adds a static 8% SST on the header total, so tax is computed once on the subtotal.
func TestPercent(t *testing.T) {
	cases := []struct {
		name     string
		subtotal Amount
		pct      int64
		want     Amount
	}{
		{name: "166.50 SST is exactly 13.32", subtotal: 16650, pct: 8, want: 1332},
		{name: "60.00 SST", subtotal: 6000, pct: 8, want: 480},
		{name: "0.08 cents rounds to 0", subtotal: 1, pct: 8, want: 0},
		{name: "0.56 cents rounds to 1", subtotal: 7, pct: 8, want: 1},
		{name: "1.00 SST", subtotal: 100, pct: 8, want: 8},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.subtotal.Percent(c.pct); got != c.want {
				t.Errorf("Amount(%d).Percent(%d) = %d, want %d", c.subtotal, c.pct, got, c.want)
			}
		})
	}
}

// Rounding per line then summing must match the stored line totals, or the ERP
// header disagrees by a cent.
func TestLineTotals(t *testing.T) {
	cases := []struct {
		name  string
		price string
		qty   int64
		bps   int64
		lines int
		want  Amount
	}{
		{name: "18.50 x 10 less 10% is exactly 166.50", price: "18.5", qty: 10, bps: 1000, lines: 1, want: 16650},
		{name: "0.335 rounds to 34 cents on every one of three lines", price: "0.335", qty: 1, lines: 3, want: 102},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			unit, err := ParseDecimal(c.price)
			if err != nil {
				t.Fatal(err)
			}

			var subtotal Amount
			for range c.lines {
				subtotal += unit.Mul(c.qty).ApplyDiscount(c.bps)
			}

			if subtotal != c.want {
				t.Fatalf("subtotal = %d (%s), want %d", subtotal, subtotal, c.want)
			}
		})
	}
}

// Decimal decodes the major-unit prices ZOLO order payloads carry.
func TestDecimalUnmarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Amount
	}{
		{name: "raw decimal", raw: `{"unit_price": 18.5}`, want: 1850},
		{name: "quoted decimal", raw: `{"unit_price": "12.00"}`, want: 1200},
		{name: "integer", raw: `{"unit_price": 12}`, want: 1200},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var payload struct {
				UnitPrice Decimal `json:"unit_price"`
			}
			if err := json.Unmarshal([]byte(c.raw), &payload); err != nil {
				t.Fatal(err)
			}
			if got := payload.UnitPrice.Amount(); got != c.want {
				t.Errorf("decoded %s = %d, want %d", c.raw, got, c.want)
			}
		})
	}
}

// Amount is minor units in both directions. An asymmetric codec multiplies a
// value by 100 every time it passes through an audit snapshot.
func TestAmountJSONRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		amount   Amount
		wantJSON string
	}{
		{name: "line total", amount: 16650, wantJSON: `{"line_total":16650}`},
		{name: "zero", amount: 0, wantJSON: `{"line_total":0}`},
		{name: "negative", amount: -1332, wantJSON: `{"line_total":-1332}`},
	}

	type envelope struct {
		LineTotal Amount `json:"line_total"`
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := json.Marshal(envelope{LineTotal: c.amount})
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != c.wantJSON {
				t.Fatalf("marshalled = %s, want %s", out, c.wantJSON)
			}

			var back envelope
			if err := json.Unmarshal(out, &back); err != nil {
				t.Fatal(err)
			}
			if back.LineTotal != c.amount {
				t.Fatalf("round-tripped = %d, want %d", back.LineTotal, c.amount)
			}
		})
	}
}

func TestString(t *testing.T) {
	cases := []struct {
		name string
		in   Amount
		want string
	}{
		{name: "hundreds", in: 16650, want: "166.50"},
		{name: "cents only", in: 5, want: "0.05"},
		{name: "whole unit", in: 100, want: "1.00"},
		{name: "negative", in: -1332, want: "-13.32"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.String(); got != c.want {
				t.Errorf("Amount(%d).String() = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSupported(t *testing.T) {
	cases := []struct {
		name     string
		currency string
		want     bool
	}{
		{name: "MYR", currency: "MYR", want: true},
		{name: "lowercase", currency: "myr", want: true},
		{name: "padded", currency: " SGD ", want: true},
		{name: "USD", currency: "USD", want: true},
		{name: "JPY has no minor unit", currency: "JPY"},
		{name: "KWD has three decimals", currency: "KWD"},
		{name: "empty", currency: ""},
		{name: "unknown code", currency: "XXX"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Supported(c.currency); got != c.want {
				t.Errorf("Supported(%q) = %v, want %v", c.currency, got, c.want)
			}
		})
	}
}
