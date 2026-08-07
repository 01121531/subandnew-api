package systemupdate

import "time"

const (
	RepositoryOwner  = "01121531"
	RepositoryName   = "subandnew-api"
	RepositoryURL    = "https://github.com/01121531/subandnew-api"
	LatestReleaseURL = "https://api.github.com/repos/01121531/subandnew-api/releases/latest"

	helperCommand = "__huichuan_apply_update"
)

type Capability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
	Platform  string `json:"platform"`
	Arch      string `json:"arch"`
}

type ReleaseInfo struct {
	ID              int64  `json:"id"`
	TagName         string `json:"tag_name"`
	Name            string `json:"name,omitempty"`
	Body            string `json:"body,omitempty"`
	HTMLURL         string `json:"html_url,omitempty"`
	PublishedAt     string `json:"published_at,omitempty"`
	CurrentVersion  string `json:"current_version"`
	UpdateAvailable bool   `json:"update_available"`
	Installable     bool   `json:"installable"`
	Reason          string `json:"reason,omitempty"`
	AssetName       string `json:"asset_name,omitempty"`
}

type UpdatePhase string

const (
	PhaseIdle        UpdatePhase = "idle"
	PhaseDownloading UpdatePhase = "downloading"
	PhaseVerifying   UpdatePhase = "verifying"
	PhaseStaged      UpdatePhase = "staged"
	PhaseRestarting  UpdatePhase = "restarting"
	PhaseValidating  UpdatePhase = "validating"
	PhaseSucceeded   UpdatePhase = "succeeded"
	PhaseFailed      UpdatePhase = "failed"
	PhaseRollingBack UpdatePhase = "rolling_back"
	PhaseRolledBack  UpdatePhase = "rolled_back"
)

type UpdateState struct {
	TaskID          string      `json:"task_id,omitempty"`
	Phase           UpdatePhase `json:"phase"`
	Progress        int         `json:"progress"`
	CurrentVersion  string      `json:"current_version,omitempty"`
	TargetVersion   string      `json:"target_version,omitempty"`
	ReleaseID       int64       `json:"release_id,omitempty"`
	MessageCode     string      `json:"message_code,omitempty"`
	ErrorCode       string      `json:"error_code,omitempty"`
	StartedAt       int64       `json:"started_at,omitempty"`
	UpdatedAt       int64       `json:"updated_at,omitempty"`
	CompletedAt     int64       `json:"completed_at,omitempty"`
	RestartRequired bool        `json:"restart_required"`
}

func (s UpdateState) Active() bool {
	switch s.Phase {
	case PhaseDownloading, PhaseVerifying, PhaseStaged, PhaseRestarting, PhaseValidating, PhaseRollingBack:
		return true
	default:
		return false
	}
}

type githubRelease struct {
	ID          int64         `json:"id"`
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

type updatePlan struct {
	TaskID                 string   `json:"task_id"`
	ParentPID              int      `json:"parent_pid"`
	TargetPath             string   `json:"target_path"`
	StagedPath             string   `json:"staged_path"`
	BackupPath             string   `json:"backup_path"`
	StatePath              string   `json:"state_path"`
	ReadyPath              string   `json:"ready_path"`
	WorkingDir             string   `json:"working_dir"`
	Args                   []string `json:"args"`
	HealthURL              string   `json:"health_url"`
	HealthTimeoutSeconds   int      `json:"health_timeout_seconds"`
	ShutdownTimeoutSeconds int      `json:"shutdown_timeout_seconds"`
	CurrentVersion         string   `json:"current_version"`
	TargetVersion          string   `json:"target_version"`
	PreviousSHA256         string   `json:"previous_sha256"`
	ExpectedSHA256         string   `json:"expected_sha256"`
	ReleaseID              int64    `json:"release_id"`
	StartedAt              int64    `json:"started_at"`
}

var now = func() time.Time { return time.Now() }
