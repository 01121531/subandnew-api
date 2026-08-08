package systemupdate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{"v1.0.1", "v1.0.0", 1},
		{"v1.0.0", "v1.0.0", 0},
		{"v1.2.0", "v2.0.0", -1},
		{"v1.0.0", "v1.0.0-rc.1", 1},
		{"v1.0.0", "development", 1},
		{"api", "v1.0.0", 0},
	}
	for _, test := range tests {
		t.Run(test.left+"_"+test.right, func(t *testing.T) {
			require.Equal(t, test.want, compareVersions(test.left, test.right))
		})
	}
	require.True(t, isSemanticVersion("v12.34.56"))
	require.False(t, isSemanticVersion("api"))
	require.False(t, isSemanticVersion("v01.2.3"))
}

func TestSelectAssets(t *testing.T) {
	release := githubRelease{
		TagName: "v1.2.3",
		Assets: []githubAsset{
			{ID: 1, Name: "checksums-windows.txt"},
			{ID: 3, Name: "subandnew-api-v1.2.3-windows-amd64.exe"},
		},
	}
	binary, checksum, err := selectAssets(release)
	require.NoError(t, err)
	require.Equal(t, int64(3), binary.ID)
	require.Equal(t, int64(1), checksum.ID)

	release.Assets = release.Assets[:1]
	_, _, err = selectAssets(release)
	require.Error(t, err)
}

func TestPlatformReleaseAssetNames(t *testing.T) {
	require.True(t, platformSupported("windows", "amd64"))
	require.True(t, platformSupported("linux", "amd64"))
	require.True(t, platformSupported("linux", "arm64"))
	require.True(t, platformSupported("darwin", "amd64"))
	require.True(t, platformSupported("darwin", "arm64"))
	require.False(t, platformSupported("windows", "arm64"))
	require.False(t, platformSupported("freebsd", "amd64"))

	require.Equal(t, "windows", releaseArtifactPlatform("windows"))
	require.Equal(t, "linux", releaseArtifactPlatform("linux"))
	require.Equal(t, "macos", releaseArtifactPlatform("darwin"))
	require.Equal(t, "subandnew-api-v1.2.3-windows-amd64.exe", releaseBinaryName("v1.2.3", "windows", "amd64"))
	require.Equal(t, "subandnew-api-v1.2.3-linux-arm64", releaseBinaryName("v1.2.3", "linux", "arm64"))
	require.Equal(t, "subandnew-api-v1.2.3-macos-arm64", releaseBinaryName("v1.2.3", "darwin", "arm64"))
}

func TestPublicReleaseInfoShowsInvalidDifferentTag(t *testing.T) {
	info := publicReleaseInfo(githubRelease{ID: 1, TagName: "api"}, "v1.0.0")
	require.True(t, info.UpdateAvailable)
	require.False(t, info.Installable)
}

func TestChecksumForFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "checksums-windows.txt")
	hash := strings.Repeat("a", sha256.Size*2)
	require.NoError(t, os.WriteFile(path, []byte(hash+"  subandnew-api-v1.0.0-windows-amd64.exe\n"), 0600))
	actual, err := checksumForFile(path, "subandnew-api-v1.0.0-windows-amd64.exe")
	require.NoError(t, err)
	require.Equal(t, hash, actual)
	_, err = checksumForFile(path, "other.exe")
	require.Error(t, err)
}

func TestHashFileSHA256AndVerification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binary")
	require.NoError(t, os.WriteFile(path, []byte("target-version"), 0600))
	expected := sha256.Sum256([]byte("target-version"))
	expectedHex := fmt.Sprintf("%x", expected[:])

	actual, err := hashFileSHA256(path)
	require.NoError(t, err)
	require.Equal(t, expectedHex, actual)
	require.NoError(t, verifyFileSHA256(path, expectedHex))
	require.Error(t, verifyFileSHA256(path, strings.Repeat("0", sha256.Size*2)))
	require.Error(t, verifyFileSHA256(path, "invalid"))
}

func TestSystemUpdateTimeoutConfiguration(t *testing.T) {
	t.Setenv("SYSTEM_UPDATE_HEALTH_TIMEOUT_SECONDS", "")
	t.Setenv("SYSTEM_UPDATE_SHUTDOWN_TIMEOUT_SECONDS", "")
	require.Equal(t, 600, updateHealthTimeoutSeconds())
	require.Equal(t, 300, ShutdownTimeoutSeconds())
	require.Equal(t, 10*time.Minute, normalizedHealthTimeout(600))
	require.Equal(t, 5*time.Minute+processExitGracePeriod, processExitTimeout(300))

	t.Setenv("SYSTEM_UPDATE_HEALTH_TIMEOUT_SECONDS", "900")
	t.Setenv("SYSTEM_UPDATE_SHUTDOWN_TIMEOUT_SECONDS", "420")
	require.Equal(t, 900, updateHealthTimeoutSeconds())
	require.Equal(t, 420, ShutdownTimeoutSeconds())

	t.Setenv("SYSTEM_UPDATE_HEALTH_TIMEOUT_SECONDS", "10")
	t.Setenv("SYSTEM_UPDATE_SHUTDOWN_TIMEOUT_SECONDS", "99999")
	require.Equal(t, 600, updateHealthTimeoutSeconds())
	require.Equal(t, 300, ShutdownTimeoutSeconds())
}

func TestServiceManagedValidationFailureStaysActive(t *testing.T) {
	originalNow := now
	defer func() { now = originalNow }()
	now = func() time.Time { return time.Unix(3_000_000, 0) }

	state := deferServiceManagedValidation(UpdateState{
		TaskID:          "update_test",
		Phase:           PhaseRollingBack,
		Progress:        98,
		ErrorCode:       "new_version_unhealthy",
		MessageCode:     "rolling_back",
		RestartRequired: true,
		CompletedAt:     now().Add(-time.Minute).Unix(),
	})
	require.Equal(t, PhaseValidating, state.Phase)
	require.Equal(t, 97, state.Progress)
	require.Equal(t, "waiting_for_service_manager", state.MessageCode)
	require.Empty(t, state.ErrorCode)
	require.True(t, state.RestartRequired)
	require.Zero(t, state.CompletedAt)
	require.Equal(t, now().Unix(), state.UpdatedAt)
	require.True(t, state.Active())
}

func TestReadUpdatePlanRequiresBinaryHashes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	plan := updatePlan{
		TaskID: "update_test", ParentPID: 123,
		TargetPath: "target", StagedPath: "staged", BackupPath: "backup",
		StatePath: "state", ReadyPath: "ready", HealthURL: "http://127.0.0.1/status",
		TargetVersion: "v1.0.8",
	}
	data, err := json.Marshal(plan)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	_, err = readUpdatePlan(path)
	require.Error(t, err)

	plan.PreviousSHA256 = strings.Repeat("a", sha256.Size*2)
	plan.ExpectedSHA256 = strings.Repeat("b", sha256.Size*2)
	data, err = json.Marshal(plan)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	actual, err := readUpdatePlan(path)
	require.NoError(t, err)
	require.Equal(t, plan.PreviousSHA256, actual.PreviousSHA256)
	require.Equal(t, plan.ExpectedSHA256, actual.ExpectedSHA256)
}

func TestSaveStateReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, saveState(path, UpdateState{Phase: PhaseDownloading, Progress: 10}))
	require.NoError(t, saveState(path, UpdateState{Phase: PhaseSucceeded, Progress: 100}))
	state, err := loadState(path)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, state.Phase)
	require.Equal(t, 100, state.Progress)
}

func TestValidateDownloadURL(t *testing.T) {
	require.NoError(t, validateDownloadURL("https://github.com/01121531/subandnew-api/releases/download/v1.0.0/app.exe"))
	require.NoError(t, validateDownloadURL("https://release-assets.githubusercontent.com/example"))
	require.Error(t, validateDownloadURL("http://github.com/file"))
	require.Error(t, validateDownloadURL("https://example.com/file"))
	require.Error(t, validateDownloadURL("https://github.com@example.com/file"))
}

func TestUpdateStateActive(t *testing.T) {
	require.True(t, UpdateState{Phase: PhaseRestarting}.Active())
	require.False(t, UpdateState{Phase: PhaseSucceeded}.Active())
	require.False(t, UpdateState{Phase: PhaseFailed}.Active())
}

func TestNormalizeStaleActiveState(t *testing.T) {
	originalNow := now
	defer func() { now = originalNow }()
	now = func() time.Time { return time.Unix(1_000_000, 0) }

	fresh, changed := normalizeStaleActiveState(UpdateState{
		Phase:     PhaseRestarting,
		Progress:  94,
		UpdatedAt: now().Add(-time.Minute).Unix(),
	})
	require.False(t, changed)
	require.Equal(t, PhaseRestarting, fresh.Phase)

	stale, changed := normalizeStaleActiveState(UpdateState{
		Phase:     PhaseRestarting,
		Progress:  94,
		UpdatedAt: now().Add(-activeStateStaleAfter - time.Second).Unix(),
	})
	require.True(t, changed)
	require.Equal(t, PhaseFailed, stale.Phase)
	require.Equal(t, "stale_update_state", stale.ErrorCode)
	require.Equal(t, 0, stale.Progress)
	require.False(t, stale.RestartRequired)
}

func TestRecoverInterruptedUpdateMarksMatchingVersionSucceeded(t *testing.T) {
	originalNow := now
	defer func() { now = originalNow }()
	now = func() time.Time { return time.Unix(2_000_000, 0) }

	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, saveState(path, UpdateState{
		TaskID:          "update_test",
		Phase:           PhaseRestarting,
		Progress:        94,
		CurrentVersion:  "98f68d2",
		TargetVersion:   "v1.0.5",
		ReleaseID:       1005,
		MessageCode:     "waiting_for_shutdown",
		StartedAt:       now().Add(-2 * time.Minute).Unix(),
		UpdatedAt:       now().Add(-time.Minute).Unix(),
		RestartRequired: true,
	}))

	state, changed := recoverInterruptedUpdateAt(path, "v1.0.5")
	require.True(t, changed)
	require.Equal(t, PhaseSucceeded, state.Phase)
	require.Equal(t, 100, state.Progress)
	require.Equal(t, "succeeded", state.MessageCode)
	require.Empty(t, state.ErrorCode)
	require.False(t, state.RestartRequired)
	require.Equal(t, now().Unix(), state.UpdatedAt)
	require.Equal(t, now().Unix(), state.CompletedAt)

	saved, err := loadState(path)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, saved.Phase)
	require.Equal(t, 100, saved.Progress)
}

func TestRecoverInterruptedUpdateDoesNotSucceedMismatchedVersion(t *testing.T) {
	originalNow := now
	defer func() { now = originalNow }()
	now = func() time.Time { return time.Unix(2_000_000, 0) }

	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, saveState(path, UpdateState{
		Phase:           PhaseRestarting,
		Progress:        94,
		TargetVersion:   "v1.0.5",
		UpdatedAt:       now().Add(-time.Minute).Unix(),
		RestartRequired: true,
	}))

	state, changed := recoverInterruptedUpdateAt(path, "v1.0.4")
	require.False(t, changed)
	require.Equal(t, PhaseRestarting, state.Phase)
	require.Equal(t, 94, state.Progress)
	require.True(t, state.RestartRequired)
}

func TestRecoverInterruptedUpdateRepairsLateServiceManagerStartup(t *testing.T) {
	originalNow := now
	defer func() { now = originalNow }()
	now = func() time.Time { return time.Unix(4_000_000, 0) }

	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, saveState(path, UpdateState{
		TaskID:         "update_late_start",
		Phase:          PhaseFailed,
		Progress:       0,
		CurrentVersion: "v1.0.7",
		TargetVersion:  "v1.0.8",
		ErrorCode:      "stale_update_state",
		MessageCode:    "failed",
		StartedAt:      now().Add(-45 * time.Minute).Unix(),
		UpdatedAt:      now().Add(-15 * time.Minute).Unix(),
		CompletedAt:    now().Add(-15 * time.Minute).Unix(),
	}))

	state, changed := recoverInterruptedUpdateAt(path, "v1.0.8")
	require.True(t, changed)
	require.Equal(t, PhaseSucceeded, state.Phase)
	require.Equal(t, 100, state.Progress)
	require.Empty(t, state.ErrorCode)
	require.False(t, state.RestartRequired)
}

func TestRecoverInterruptedUpdateDoesNotHideRollbackSplitFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, saveState(path, UpdateState{
		TaskID:         "update_split",
		Phase:          PhaseFailed,
		CurrentVersion: "v1.0.6",
		TargetVersion:  "v1.0.7",
		ErrorCode:      "rollback_start_failed",
		MessageCode:    "manual_recovery_required",
	}))

	state, changed := recoverInterruptedUpdateAt(path, "v1.0.7")
	require.False(t, changed)
	require.Equal(t, PhaseFailed, state.Phase)
	require.Equal(t, "rollback_start_failed", state.ErrorCode)
}
