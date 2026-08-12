//go:build linux || darwin

package systemupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
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
	healthTimeout := normalizedHealthTimeout(plan.HealthTimeoutSeconds)
	serviceManaged := serviceManagerWillRestart()

	// On Unix-like systems a running executable can be renamed safely. Replace
	// the target before the old process exits so service managers such as
	// systemd Restart=always will restart the already-updated binary instead of
	// racing the helper by immediately launching the previous file again.
	if err := verifyFileSHA256(plan.TargetPath, plan.PreviousSHA256); err != nil {
		return failPlanState(plan, state, "current_binary_changed", err)
	}
	if err := verifyFileSHA256(plan.StagedPath, plan.ExpectedSHA256); err != nil {
		return failPlanState(plan, state, "staged_binary_changed", err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.BackupPath), 0700); err != nil {
		return failPlanState(plan, state, "backup_directory_unavailable", err)
	}
	_ = os.Remove(plan.BackupPath)
	if err := os.Rename(plan.TargetPath, plan.BackupPath); err != nil {
		return failPlanState(plan, state, "backup_failed", err)
	}
	if err := os.Chmod(plan.StagedPath, 0700); err != nil {
		_ = os.Rename(plan.BackupPath, plan.TargetPath)
		return failPlanState(plan, state, "binary_permission_failed", err)
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

	// The manager treats this marker as permission to stop the parent process.
	// systemd may kill every remaining process in the service cgroup as soon as
	// the parent exits, so the verified target must already be in place first.
	if err := os.WriteFile(plan.ReadyPath, []byte("ready\n"), 0600); err != nil {
		_ = os.Remove(plan.TargetPath)
		_ = os.Rename(plan.BackupPath, plan.TargetPath)
		return failPlanState(plan, state, "helper_ready_failed", err)
	}

	if err := ensureProcessExit(plan.ParentPID, processExitTimeout(plan.ShutdownTimeoutSeconds)); err != nil {
		_ = os.Remove(plan.TargetPath)
		_ = os.Rename(plan.BackupPath, plan.TargetPath)
		state.Phase = PhaseFailed
		state.Progress = 0
		state.ErrorCode = "server_shutdown_timeout"
		state.MessageCode = "failed"
		state.CompletedAt = now().Unix()
		writeState()
		return err
	}

	state.Phase = PhaseValidating
	state.Progress = 97
	state.MessageCode = "validating"
	writeState()

	// If a service manager restarted the program after the old process exited,
	// the health endpoint may already be served by the target version. Prefer
	// that path; otherwise start the binary ourselves for plain standalone runs.
	var newProcess *os.Process
	initialHealthTimeout := 12 * time.Second
	if serviceManaged {
		initialHealthTimeout = healthTimeout
	}
	err = waitForHealthyVersion(plan.HealthURL, plan.TargetVersion, initialHealthTimeout)
	if err != nil {
		if serviceManaged {
			err = fmt.Errorf("service manager did not restart a healthy target version: %w", err)
		} else {
			newProcess, err = startApplication(plan)
		}
	}
	if err == nil && newProcess != nil {
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

	// systemd owns the service lifecycle. Replacing the on-disk binary while a
	// target process is still starting can create a split state where memory
	// runs the new version but the next restart loads the old version. Keep the
	// verified target on disk and leave validation active; a late successful
	// startup reconciles the state through RecoverInterruptedUpdate. A truly
	// broken service eventually becomes stale and requires an explicit operator
	// rollback instead of an unsafe automatic race with Restart=always.
	if serviceManaged {
		state = deferServiceManagedValidation(state)
		writeState()
		return fmt.Errorf("target version is still not healthy; automatic rollback was deferred to preserve disk/runtime consistency: %w", err)
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
	process, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	deadline := now().Add(timeout)
	for now().Before(deadline) {
		if processIsZombie(pid) {
			return nil
		}
		err = process.Signal(syscall.Signal(0))
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("timed out waiting for the server process to exit")
}

func ensureProcessExit(pid int, gracefulTimeout time.Duration) error {
	if err := waitForProcessExit(pid, gracefulTimeout); err == nil {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	// The application has already been asked to shut down internally. A stale
	// HTTP stream must not leave an online update at 94% indefinitely.
	_ = process.Signal(syscall.SIGTERM)
	if err := waitForProcessExit(pid, 10*time.Second); err == nil {
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

func processIsZombie(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// /proc/<pid>/stat format: pid (comm) state ...
	rightParen := -1
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == ')' {
			rightParen = i
			break
		}
	}
	if rightParen < 0 || rightParen+2 >= len(data) {
		return false
	}
	return data[rightParen+2] == 'Z'
}

func serviceManagerWillRestart() bool {
	return os.Getenv("INVOCATION_ID") != "" || os.Getenv("JOURNAL_STREAM") != "" || os.Getenv("SYSTEMD_EXEC_PID") != ""
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
