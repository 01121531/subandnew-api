package controller

import (
	"testing"

	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/stretchr/testify/require"
)

func TestPortalSummaryOnlyExposesAuthorizedMetrics(t *testing.T) {
	result := &managedaccount.Result{Summary: managedaccount.Summary{Total: 8, Available: 5, Unavailable: 3,
		Requests: 120, Tokens: 240, Amounts: map[string]float64{"USD": 12.5}}}
	limited := portalSummary(result, []string{"name"})
	require.Equal(t, 8, limited["total"])
	require.NotContains(t, limited, "available")
	require.NotContains(t, limited, "requests")
	require.NotContains(t, limited, "tokens")
	require.NotContains(t, limited, "amounts")

	allowed := portalSummary(result, []string{"available", "requests", "amount"})
	require.Equal(t, 5, allowed["available"])
	require.Equal(t, float64(120), allowed["requests"])
	require.Equal(t, map[string]float64{"USD": 12.5}, allowed["amounts"])
	require.NotContains(t, allowed, "tokens")
}
