package billingalert

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type stubRateProvider struct {
	name  string
	quote RateQuote
	err   error
}

func (provider stubRateProvider) Name() string { return provider.name }
func (provider stubRateProvider) Latest(context.Context) (RateQuote, error) {
	return provider.quote, provider.err
}

func TestParseECBUSDToCNYUsesSameObservationDate(t *testing.T) {
	body := []byte("CURRENCY,TIME_PERIOD,OBS_VALUE\nUSD,2026-08-12,1.2\nCNY,2026-08-12,8.64\nUSD,2026-08-11,1.1\nCNY,2026-08-11,7.7\n")
	quote, err := parseECBUSDToCNY(body)
	require.NoError(t, err)
	require.Equal(t, "7.20000000", quote.Rate)
	require.Equal(t, "2026-08-12", quote.ObservedDate)
	require.Equal(t, ExchangeSourceECB, quote.Source)
}

func TestParseECBUSDToCNYRejectsIncompleteRows(t *testing.T) {
	_, err := parseECBUSDToCNY([]byte("CURRENCY,TIME_PERIOD,OBS_VALUE\nUSD,2026-08-12,1.2\n"))
	require.ErrorIs(t, err, ErrExchangeRateUnavailable)
}

func TestFetchLatestRateFallsBackWithoutChangingSource(t *testing.T) {
	quote, err := FetchLatestRate(context.Background(),
		stubRateProvider{name: "primary", err: errors.New("offline")},
		stubRateProvider{name: "fallback", quote: RateQuote{
			Base: "USD", Quote: "CNY", Rate: "7.2", ObservedDate: "2026-08-12", Source: "fallback",
		}},
	)
	require.NoError(t, err)
	require.True(t, quote.Fallback)
	require.Equal(t, "fallback", quote.Source)
}
