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
}

func TestCalculateCNYRejectsInvalidOrNegativeValues(t *testing.T) {
	_, err := CalculateCNY("-1", "1", "7")
	require.ErrorIs(t, err, ErrInvalidDecimal)
	_, err = CalculateCNY("one", "1", "7")
	require.True(t, errors.Is(err, ErrInvalidDecimal))
}

func TestDecimalComparisonAndAddition(t *testing.T) {
	result, err := AddDecimal("0.1", "0.2", "1.000000001")
	require.NoError(t, err)
	require.Equal(t, "1.30000000", result)
	comparison, err := CompareDecimal("100.00000001", "100")
	require.NoError(t, err)
	require.Equal(t, 1, comparison)
}
