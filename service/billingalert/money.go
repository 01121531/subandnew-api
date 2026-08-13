package billingalert

import (
	"errors"
	"math/big"
	"strings"
)

const MoneyScale = 8

var ErrInvalidDecimal = errors.New("invalid decimal value")

func CalculateCNY(usd string, discountRate string, exchangeRate string) (string, error) {
	usdValue, err := parseNonNegativeDecimal(usd)
	if err != nil {
		return "", err
	}
	discountValue, err := parseNonNegativeDecimal(discountRate)
	if err != nil {
		return "", err
	}
	rateValue, err := parseNonNegativeDecimal(exchangeRate)
	if err != nil {
		return "", err
	}
	result := new(big.Rat).Mul(usdValue, discountValue)
	result.Mul(result, rateValue)
	return formatDecimal(result, MoneyScale), nil
}

func CompareDecimal(left string, right string) (int, error) {
	leftValue, err := parseNonNegativeDecimal(left)
	if err != nil {
		return 0, err
	}
	rightValue, err := parseNonNegativeDecimal(right)
	if err != nil {
		return 0, err
	}
	return leftValue.Cmp(rightValue), nil
}

func AddDecimal(values ...string) (string, error) {
	total := new(big.Rat)
	for _, value := range values {
		parsed, err := parseNonNegativeDecimal(value)
		if err != nil {
			return "", err
		}
		total.Add(total, parsed)
	}
	return formatDecimal(total, MoneyScale), nil
}

func parseNonNegativeDecimal(value string) (*big.Rat, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ErrInvalidDecimal
	}
	parsed, ok := new(big.Rat).SetString(value)
	if !ok || parsed.Sign() < 0 {
		return nil, ErrInvalidDecimal
	}
	return parsed, nil
}

func formatDecimal(value *big.Rat, scale int) string {
	if value == nil {
		return "0.00000000"
	}
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	scaledNumerator := new(big.Int).Mul(value.Num(), factor)
	quotient, remainder := new(big.Int).QuoRem(scaledNumerator, value.Denom(), new(big.Int))
	doubledRemainder := new(big.Int).Mul(new(big.Int).Abs(remainder), big.NewInt(2))
	if doubledRemainder.Cmp(new(big.Int).Abs(value.Denom())) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	digits := quotient.String()
	for len(digits) <= scale {
		digits = "0" + digits
	}
	return digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
}
