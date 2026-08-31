package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestControlPlaneLeaseAcquisitionAndFailover(t *testing.T) {
	db := useControlPlaneMigrationTestDB(t)
	require.NoError(t, db.AutoMigrate(&ControlPlaneLease{}))

	acquired, err := TryAcquireControlPlaneLease("collectors", "node-a", 100, 130)
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = TryAcquireControlPlaneLease("collectors", "node-b", 110, 140)
	require.NoError(t, err)
	require.False(t, acquired)

	renewed, err := RenewControlPlaneLease("collectors", "node-a", 120, 150)
	require.NoError(t, err)
	require.True(t, renewed)

	acquired, err = TryAcquireControlPlaneLease("collectors", "node-b", 151, 181)
	require.NoError(t, err)
	require.True(t, acquired)

	renewed, err = RenewControlPlaneLease("collectors", "node-a", 152, 182)
	require.NoError(t, err)
	require.False(t, renewed)
	require.NoError(t, ReleaseControlPlaneLease("collectors", "node-b"))
}
