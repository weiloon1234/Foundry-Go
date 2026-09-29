package money

import (
	"bytes"
	"encoding/json"
	"io"
	"math/big"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxAllocationParts bounds Allocate and Split results.
const MaxAllocationParts = 1024

// Money is an exact amount in one currency whose scale never exceeds that
// currency's minor units. The zero value has no currency and is invalid. Values
// are comparable: == means the same currency and numeric amount.
type Money struct {
	amount   decimal.Decimal
	currency Currency
}

// New accepts an amount that already fits the currency's minor units. It never
// rounds; use NewRounded to choose a rounding mode explicitly.
func New(amount decimal.Decimal, c Currency) (Money, error) {
	minor, err := c.MinorUnits()
	if err != nil {
		return Money{}, err
	}
	if amount.Scale() > minor {
		return Money{}, fault.New(fault.Invalid, "money amount has more fractional digits than its currency")
	}
	return Money{amount: amount, currency: c}, nil
}

// NewRounded rounds amount to the currency's minor units with mode.
func NewRounded(amount decimal.Decimal, c Currency, mode decimal.RoundingMode) (Money, error) {
	minor, err := c.MinorUnits()
	if err != nil {
		return Money{}, err
	}
	rounded, err := amount.Round(minor, mode)
	if err != nil {
		return Money{}, err
	}
	return Money{amount: rounded, currency: c}, nil
}

// FromMinor converts integer minor units exactly, for example 1234 USD cents
// to 12.34 USD.
func FromMinor(units int64, c Currency) (Money, error) {
	minor, err := c.MinorUnits()
	if err != nil {
		return Money{}, err
	}
	amount, err := decimal.Scaled(units, minor)
	if err != nil {
		return Money{}, err
	}
	return Money{amount: amount, currency: c}, nil
}

// Zero returns a zero amount in currency c.
func Zero(c Currency) (Money, error) { return New(decimal.Decimal{}, c) }

func (m Money) Amount() decimal.Decimal { return m.amount }
func (m Money) Currency() Currency      { return m.currency }
func (m Money) IsZero() bool            { return m.amount.IsZero() }
func (m Money) Sign() int               { return m.amount.Sign() }
func (m Money) Neg() Money              { return Money{amount: m.amount.Neg(), currency: m.currency} }
func (m Money) Abs() Money              { return Money{amount: m.amount.Abs(), currency: m.currency} }

// Validate reports a zero-value or otherwise inconsistent Money.
func (m Money) Validate() error {
	_, err := New(m.amount, m.currency)
	return err
}

// MinorUnits returns the amount as an integer count of minor units. Amounts
// beyond int64 fail instead of wrapping.
func (m Money) MinorUnits() (int64, error) {
	units, err := m.minorCoefficient()
	if err != nil {
		return 0, err
	}
	if !units.IsInt64() {
		return 0, fault.New(fault.Invalid, "money minor units exceed int64")
	}
	return units.Int64(), nil
}

func (m Money) minorCoefficient() (*big.Int, error) {
	minor, err := m.currency.MinorUnits()
	if err != nil {
		return nil, err
	}
	text, err := m.amount.Fixed(minor)
	if err != nil {
		return nil, err
	}
	digits := make([]byte, 0, len(text))
	for i := range len(text) {
		if text[i] != '.' {
			digits = append(digits, text[i])
		}
	}
	units, _ := new(big.Int).SetString(string(digits), 10)
	return units, nil
}

func (m Money) same(other Money) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := other.Validate(); err != nil {
		return err
	}
	if m.currency != other.currency {
		return fault.New(fault.Invalid, "money currencies differ")
	}
	return nil
}

// Add returns the exact sum of two amounts in the same currency.
func (m Money) Add(other Money) (Money, error) {
	if err := m.same(other); err != nil {
		return Money{}, err
	}
	sum, err := m.amount.Add(other.amount)
	if err != nil {
		return Money{}, err
	}
	return Money{amount: sum, currency: m.currency}, nil
}

// Sub returns the exact difference of two amounts in the same currency.
func (m Money) Sub(other Money) (Money, error) {
	if err := m.same(other); err != nil {
		return Money{}, err
	}
	difference, err := m.amount.Sub(other.amount)
	if err != nil {
		return Money{}, err
	}
	return Money{amount: difference, currency: m.currency}, nil
}

// Cmp compares two amounts in the same currency, returning -1, 0 or 1.
func (m Money) Cmp(other Money) (int, error) {
	if err := m.same(other); err != nil {
		return 0, err
	}
	return m.amount.Cmp(other.amount), nil
}

// Multiply scales the amount by an exact factor, such as a quantity or tax
// rate, and rounds the product to minor units with mode.
func (m Money) Multiply(factor decimal.Decimal, mode decimal.RoundingMode) (Money, error) {
	if err := m.Validate(); err != nil {
		return Money{}, err
	}
	product, err := m.amount.Mul(factor)
	if err != nil {
		return Money{}, err
	}
	return NewRounded(product, m.currency, mode)
}

// Allocate splits the amount by non-negative integer ratios without creating
// or losing minor units. Each part receives the floor of its exact share; the
// remaining minor units go one each to the parts with the largest discarded
// remainders, earlier parts first on ties. Negative amounts allocate their
// magnitude and keep the sign.
func (m Money) Allocate(ratios ...int64) ([]Money, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if len(ratios) == 0 || len(ratios) > MaxAllocationParts {
		return nil, invalidAllocation()
	}
	total := new(big.Int)
	for _, ratio := range ratios {
		if ratio < 0 {
			return nil, invalidAllocation()
		}
		total.Add(total, big.NewInt(ratio))
	}
	if total.Sign() == 0 {
		return nil, invalidAllocation()
	}
	units, err := m.minorCoefficient()
	if err != nil {
		return nil, err
	}
	negative := units.Sign() < 0
	units.Abs(units)
	shares := make([]*big.Int, len(ratios))
	remainders := make([]*big.Int, len(ratios))
	assigned := new(big.Int)
	for i, ratio := range ratios {
		share, remainder := new(big.Int).QuoRem(new(big.Int).Mul(units, big.NewInt(ratio)), total, new(big.Int))
		shares[i], remainders[i] = share, remainder
		assigned.Add(assigned, share)
	}
	// Fewer than len(ratios) units remain, so each chosen part gains at most one.
	for left := new(big.Int).Sub(units, assigned); left.Sign() > 0; left.Sub(left, big.NewInt(1)) {
		best := -1
		for i, remainder := range remainders {
			if remainder.Sign() > 0 && (best < 0 || remainder.Cmp(remainders[best]) > 0) {
				best = i
			}
		}
		shares[best].Add(shares[best], big.NewInt(1))
		remainders[best].SetInt64(0)
	}
	minor, _ := m.currency.MinorUnits()
	unit, err := decimal.Scaled(1, minor)
	if err != nil {
		return nil, err
	}
	result := make([]Money, len(shares))
	for i, share := range shares {
		if negative {
			share.Neg(share)
		}
		amount, err := decimal.Parse(share.String())
		if err == nil {
			amount, err = amount.Mul(unit)
		}
		if err != nil {
			return nil, err
		}
		result[i] = Money{amount: amount, currency: m.currency}
	}
	return result, nil
}

// Split divides the amount into parts equal shares; see Allocate.
func (m Money) Split(parts int) ([]Money, error) {
	if parts < 1 || parts > MaxAllocationParts {
		return nil, invalidAllocation()
	}
	ratios := make([]int64, parts)
	for i := range ratios {
		ratios[i] = 1
	}
	return m.Allocate(ratios...)
}

func invalidAllocation() error {
	return fault.New(fault.Invalid, "money allocation requires 1 to 1024 non-negative ratios with a positive total")
}

// wire is the JSON object form. Amount text always has the currency's minor
// unit scale, such as "12.30".
type wire struct {
	Amount   string   `json:"amount"`
	Currency Currency `json:"currency"`
}

func (m Money) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	minor, _ := m.currency.MinorUnits()
	text, err := m.amount.Fixed(minor)
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire{Amount: text, Currency: m.currency})
}

// UnmarshalJSON requires an object with exactly one "amount" decimal string and
// one "currency" code. Amounts beyond the currency's minor units are rejected.
// The receiver changes only after successful decoding.
func (m *Money) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return invalidJSON()
	}
	var amount, code *string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return invalidJSON()
		}
		var target **string
		switch token {
		case "amount":
			target = &amount
		case "currency":
			target = &code
		default:
			return invalidJSON()
		}
		if *target != nil {
			return invalidJSON()
		}
		value, err := decoder.Token()
		text, ok := value.(string)
		if err != nil || !ok {
			return invalidJSON()
		}
		*target = &text
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || amount == nil || code == nil {
		return invalidJSON()
	}
	if _, err := decoder.Token(); err != io.EOF {
		return invalidJSON()
	}
	parsedAmount, err := decimal.Parse(*amount)
	if err != nil {
		return err
	}
	c, err := ParseCurrency(*code)
	if err != nil {
		return err
	}
	parsed, err := New(parsedAmount, c)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

func invalidJSON() error {
	return fault.New(fault.Invalid, `money JSON must be an object with string "amount" and "currency" fields`)
}
