package money

import "strings"

const scale = 100

// Currencies whose minor unit is 1/100 of the major unit.
var supported = map[string]bool{
	"MYR": true,
	"SGD": true,
	"USD": true,
	"EUR": true,
	"GBP": true,
	"AUD": true,
	"THB": true,
	"PHP": true,
	"IDR": true,
}

// Supported reports whether this package can represent the given ISO-4217 currency exactly.
func Supported(currency string) bool {
	return supported[strings.ToUpper(strings.TrimSpace(currency))]
}
