//go:build windows

package systemupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

func RunHelperIfRequested() bool {
	if len(os.Args) != 3 || os.Args[1] != helperCommand {
		return false
	}
	if err := applyUpdatePlan(os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "SubAndNew updater failed: %v\n", err)
	}
	return true
}

func applyUpdatePlan(planPath string) error {
	plan, err := readUpdatePlan(planPath)
	if err != nil {
		return err
	}
	state := UpdateState{
		TaskID:          plan.TaskID,
		Phase:           PhaseRestarting,
		Progress:        94,
		CurrentVersion:  plan.CurrentVersion,
		TargetVersion:   plan.TargetVersion,
		ReleaseID:       plan.ReleaseID,
		MessageCode:     "waiting_for_shutdown",
		StartedAt:       plan.StartedAt,
		UpdatedAt:       now().Unix(),
		RestartRequired: true,
	}
	writeState := func() {
		state.UpdatedAt = now().Unix()
		_ = saveState(plan.StatePath, state)
	}
	writeState()
	if err := os.WriteFile(plan.ReadyPath, []byte("ready\n"), 0600); err != nil {
		return failPlanState(plan, state, "helper_ready_failed", err)
	}
	healthTimeout := normalizedHealthTimeout(plan.HealthTimeoutSeconds)
	if err := verifyFileSHA256(plan.TargetPath, plan.PreviousSHA256); err != nil {
		return failPlanState(plan, state, "current_binary_changed", err)
	}
	if err := verifyFileSHA256(plan.StagedPath, plan.ExpectedSHA256); err != nil {
		return failPlanState(plan, state, "staged_binary_changed", err)
	}

	if err := ensureProcessExit(plan.ParentPID, processExitTimeout(plan.ShutdownTimeoutSeconds)); err != nil {
		state.Phase = PhaseFailed
		state.Progress = 0
		state.ErrorCode = "server_shutdown_timeout"
		state.MessageCode = "failed"
		state.CompletedAt = now().Unix()
		writeState()
		return err
	}

	if err := os.MkdirAll(filepath.Dir(plan.BackupPath), 0700); err != nil {
		return failPlanState(plan, state, "backup_directory_unavailable", err)
	}
	_ = os.Remove(plan.BackupPath)
	if err := os.Rename(plan.TargetPath, plan.BackupPath); err != nil {
		return failPlanState(plan, state, "backup_failed", err)
	}
	if err := os.Rename(plan.StagedPath, plan.TargetPath); err != nil {
		_ = os.Rename(plan.BackupPath, plan.TargetPath)
		return failPlanState(plan, state, "replace_failed", err)
	}
	if err := verifyFileSHA256(plan.TargetPath, plan.ExpectedSHA256); err != nil {
		_ = os.Remove(plan.TargetPath)
		_ = os.Rename(plan.BackupPath, plan.TargetPath)
		return failPlanState(plan, state, "installed_binary_verification_failed", err)
	}

	state.Phase = PhaseValidating
	state.Progress = 97
	state.MessageCode = "validating"
	writeState()
	newProcess, err := startApplication(plan)
	if err == nil {
		err = waitForHealthyVersion(plan.HealthURL, plan.TargetVersion, healthTimeout)
	}
	if err == nil {
		err = verifyFileSHA256(plan.TargetPath, plan.ExpectedSHA256)
	}
	if err == nil {
		state.Phase = PhaseSucceeded
		state.Progress = 100
		state.MessageCode = "succeeded"
		state.RestartRequired = false
		state.CompletedAt = now().Unix()
		writeState()
		return nil
	}

	state.Phase = PhaseRollingBack
	state.Progress = 98
	state.ErrorCode = "new_version_unhealthy"
	state.MessageCode = "rolling_back"
	writeState()
	if newProcess != nil {
		_ = newProcess.Kill()
		_, _ = newProcess.Wait()
	}
	_ = os.Remove(plan.TargetPath)
	if restoreErr := os.Rename(plan.BackupPath, plan.TargetPath); restoreErr != nil {
		state.Phase = PhaseFailed
		state.Progress = 0
		state.ErrorCode = "rollback_restore_failed"
		state.MessageCode = "manual_recovery_required"
		state.CompletedAt = now().Unix()
		writeState()
		return fmt.Errorf("new version unhealthy (%v) and rollback restore failed: %w", err, restoreErr)
	}
	if restoreErr := verifyFileSHA256(plan.TargetPath, plan.PreviousSHA256); restoreErr != nil {
		state.Phase = PhaseFailed
		state.Progress = 0
		state.ErrorCode = "rollback_verification_failed"
		state.MessageCode = "manual_recovery_required"
		state.CompletedAt = now().Unix()
		writeState()
		return fmt.Errorf("new version unhealthy (%v) and rollback verification failed: %w", err, restoreErr)
	}
	oldProcess, startErr := startApplication(plan)
	if startErr == nil {
		startErr = waitForHealthyVersion(plan.HealthURL, plan.CurrentVersion, healthTimeout)
	}
	if startErr != nil {
		state.Phase = PhaseFailed
		state.Progress = 0
		state.ErrorCode = "rollback_start_failed"
		state.MessageCode = "manual_recovery_required"
		state.CompletedAt = now().Unix()
		writeState()
		if oldProcess != nil {
			_ = oldProcess.Kill()
		}
		return fmt.Errorf("new version unhealthy (%v) and rollback failed: %w", err, startErr)
	}
	state.Phase = PhaseRolledBack
	state.Progress = 100
	state.ErrorCode = "new_version_unhealthy"
	state.MessageCode = "rolled_back"
	state.RestartRequired = false
	state.CompletedAt = now().Unix()
	writeState()
	return fmt.Errorf("new version failed health validation and the previous version was restored: %w", err)
}

func readUpdatePlan(path string) (updatePlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return updatePlan{}, err
	}
	var plan updatePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return updatePlan{}, err
	}
	if plan.TaskID == "" || plan.ParentPID <= 0 || plan.TargetPath == "" || plan.StagedPath == "" || plan.BackupPath == "" || plan.StatePath == "" || plan.ReadyPath == "" || plan.HealthURL == "" || plan.TargetVersion == "" || plan.PreviousSHA256 == "" || plan.ExpectedSHA256 == "" {
		return updatePlan{}, errors.New("update plan is incomplete")
	}
	return plan, nil
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(handle)
	result, err := windows.WaitForSingleObject(handle, uint32(timeout/time.Millisecond))
	if err != nil {
		return err
	}
	if result == uint32(windows.WAIT_TIMEOUT) {
		return errors.New("timed out waiting for the server process to exit")
	}
	return nil
}

func ensureProcessExit(pid int, gracefulTimeout time.Duration) error {
	if err := waitForProcessExit(pid, gracefulTimeout); err == nil {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("force the server process to exit: %w", err)
	}
	if err := waitForProcessExit(pid, 5*time.Second); err != nil {
		return fmt.Errorf("server process remained alive after forced shutdown: %w", err)
	}
	return nil
}

func startApplication(plan updatePlan) (*os.Process, error) {
	command := exec.Command(plan.TargetPath, plan.Args...)
	command.Dir = plan.WorkingDir
	command.Env = os.Environ()
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	return command.Process, nil
}

func waitForHealthyVersion(healthURL string, expectedVersion string, timeout time.Duration) error {
	deadline := now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	for now().Before(deadline) {
		request, err := http.NewRequest(http.MethodGet, healthURL, nil)
		if err == nil {
			request.Header.Set("Cache-Control", "no-store")
			response, requestErr := client.Do(request)
			if requestErr == nil {
				var payload struct {
					Success bool `json:"success"`
					Data    struct {
						Version string `json:"version"`
					} `json:"data"`
				}
				decodeErr := json.NewDecoder(response.Body).Decode(&payload)
				response.Body.Close()
				if response.StatusCode == http.StatusOK && decodeErr == nil && payload.Success && payload.Data.Version == expectedVersion {
					return nil
				}
			}
		}
		time.Sleep(1500 * time.Millisecond)
	}
	return fmt.Errorf("version %s did not become healthy before the deadline", expectedVersion)
}

func failPlanState(plan updatePlan, state UpdateState, code string, err error) error {
	state.Phase = PhaseFailed
	state.Progress = 0
	state.ErrorCode = code
	state.MessageCode = "failed"
	state.UpdatedAt = now().Unix()
	state.CompletedAt = state.UpdatedAt
	_ = saveState(plan.StatePath, state)
	return err
}
