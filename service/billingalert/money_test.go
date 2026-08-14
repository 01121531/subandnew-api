package billingalert

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCalculateCNYUsesExactDecimalArithmetic(t *testing.T) {
	amount, err := CalculateCNY("100", "0.8", "7.2")
	require.NoError(t, err)
	require.Equal(t, "576.00000000", amount)

	amount, err = CalculateCNY("0.1", "0.1", "0.1")
	require.NoError(t, err)
	require.Equal(t, "0.00100000", amount)

	amount, err = CalculateCNY("2497.14528600", "0.55", "6.74317887")
	require.NoError(t, err)
	require.Equal(t, "9261.28353033", amount)
}

func TestCalculateCNYRejectsInvalidOrNegativeValues(t *testing.T) {
	_, err := CalculateCNY("-1", "1", "7")
	require.ErrorIs(t, err, ErrInvalidDecimal)
	_, err = CalculateCNY("one", "1", "7")
	require.True(t, errors.Is(err, ErrInvalidDecimal))
	_, err = CalculateCNY("1", "55", "7")
	require.ErrorIs(t, err, ErrInvalidDiscountRate)
}

func TestNormalizeDiscountRateAcceptsLegacyPercentages(t *testing.T) {
	tests := map[string]string{
		"55": "0.55", "100": "1", "1": "1", "0.55": "0.55", "0": "0",
	}
	for input, expected := range tests {
		normalized, err := NormalizeDiscountRate(input)
		require.NoError(t, err)
		require.Equal(t, expected, normalized)
	}
	_, err := NormalizeDiscountRate("-1")
	require.ErrorIs(t, err, ErrInvalidDecimal)
	_, err = NormalizeDiscountRate("100.01")
	require.ErrorIs(t, err, ErrInvalidDiscountRate)

	formatted, err := FormatDiscountPercent("0.55")
	require.NoError(t, err)
	require.Equal(t, "55%", formatted)
}

func TestDecimalComparisonAndAddition(t *testing.T) {
	result, err := AddDecimal("0.1", "0.2", "1.000000001")
	require.NoError(t, err)
	require.Equal(t, "1.30000000", result)
	comparison, err := CompareDecimal("100.00000001", "100")
	require.NoError(t, err)
	require.Equal(t, 1, comparison)
}
