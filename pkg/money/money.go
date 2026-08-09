package money

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// Amount is a value in minor units (cents for supported currencies).
type Amount int64

// ParseDecimal converts a decimal literal ("18.5") to minor units, rounding half away from zero.
func ParseDecimal(s string) (Amount, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("money: empty decimal")
	}

	rat, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, fmt.Errorf("money: %q is not a decimal number", s)
	}

	return fromRat(rat.Mul(rat, big.NewRat(scale, 1)))
}

// Mul multiplies by a whole quantity.
func (a Amount) Mul(qty int64) Amount {
	return a * Amount(qty)
}

// ApplyDiscount cuts the amount by bps (1000 = 10%), applied to cents so 18.50 x 0.9 x 10 lands on exactly 16650.
func (a Amount) ApplyDiscount(bps int64) Amount {
	if bps == 0 {
		return a
	}
	return a.MulRatio(10000-bps, 10000)
}

// MulRatio multiplies by num/den, rounding half away from zero.
func (a Amount) MulRatio(num, den int64) Amount {
	if den == 0 {
		panic("money: division by zero")
	}
	r := big.NewRat(int64(a), 1)
	r.Mul(r, big.NewRat(num, den))

	out, err := fromRat(r)
	if err != nil {
		panic(err) // unreachable: a is int64-bounded and callers pass small ratios
	}
	return out
}

// Percent returns pct percent of the amount, used for the SST ERP B adds to the header total.
func (a Amount) Percent(pct int64) Amount {
	return a.MulRatio(pct, 100)
}

// String renders major units, e.g. 16650 -> "166.50".
func (a Amount) String() string {
	neg := a < 0
	v := a
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d.%02d", int64(v)/scale, int64(v)%scale)
	if neg {
		return "-" + s
	}
	return s
}

// Both ERPs expect raw minor units; UnmarshalJSON must stay symmetric or every snapshot round-trip silently scales by 100.
func (a Amount) MarshalJSON() ([]byte, error) {
	return json.Marshal(int64(a))
}

func (a *Amount) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" {
		return nil
	}
	s = strings.Trim(s, `"`)

	minor, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("money: %q is not an integer minor-unit amount", s)
	}

	*a = Amount(minor)
	return nil
}

// Decimal decodes major-unit literals ("unit_price": 18.5) from ZOLO payloads, keeping the scaling at a named boundary.
type Decimal Amount

func (d Decimal) Amount() Amount { return Amount(d) }

func (d *Decimal) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" {
		return nil
	}

	// Parsed as text: a float64 hop is how 18.5 becomes 1850.0000000000002 and drifts a cent.
	parsed, err := ParseDecimal(strings.Trim(s, `"`))
	if err != nil {
		return err
	}

	*d = Decimal(parsed)
	return nil
}

// MarshalJSON renders major units so a Decimal round-trips.
func (d Decimal) MarshalJSON() ([]byte, error) {
	return json.Marshal(Amount(d).String())
}

// fromRat rounds a rational to the nearest integer, half away from zero.
func fromRat(r *big.Rat) (Amount, error) {
	num, den := r.Num(), r.Denom()

	quo, rem := new(big.Int).QuoRem(num, den, new(big.Int))

	// 2*|rem| >= |den| means at or past the halfway point.
	twiceRem := new(big.Int).Abs(rem)
	twiceRem.Lsh(twiceRem, 1)
	if twiceRem.Cmp(new(big.Int).Abs(den)) >= 0 {
		if r.Sign() < 0 {
			quo.Sub(quo, big.NewInt(1))
		} else {
			quo.Add(quo, big.NewInt(1))
		}
	}

	if !quo.IsInt64() {
		return 0, fmt.Errorf("money: value %s overflows int64 minor units", r.FloatString(2))
	}
	return Amount(quo.Int64()), nil
}
