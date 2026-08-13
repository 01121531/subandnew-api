package managedinstance

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoteDataErrorClassifiesRemoteFailures(t *testing.T) {
	require.ErrorIs(t, RemoteDataError(context.DeadlineExceeded), ErrRemoteDataUnavailable)
	require.ErrorIs(t, RemoteDataError(&ProbeError{Code: ProbeErrorRemoteHTTP}), ErrRemoteDataUnavailable)
	require.ErrorIs(t, RemoteDataError(ErrConnectorResponseLarge), ErrRemoteDataUnavailable)
	require.ErrorIs(t, RemoteDataError(ErrInvalidInstance), ErrInvalidInstance)
	require.False(t, errors.Is(RemoteDataError(ErrUnsupportedCapability), ErrRemoteDataUnavailable))
}
