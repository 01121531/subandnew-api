package managedinstance

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
)

type ConnectionStage struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type PreflightResult struct {
	Success   bool              `json:"success"`
	Probe     *ProbeResult      `json:"probe,omitempty"`
	Stages    []ConnectionStage `json:"stages"`
	ErrorCode string            `json:"error_code,omitempty"`
	Advice    string            `json:"advice,omitempty"`
}

// ProbeConnection validates and probes an instance without persisting either
// the instance or its credential.
func ProbeConnection(ctx context.Context, input CreateInput) (*PreflightResult, error) {
	if strings.TrimSpace(input.Name) == "" {
		input.Name = "preflight"
	}
	instance, err := buildInstance(
		input.Name, input.Kind, input.BaseURL, input.Environment, input.Labels,
		input.ManagementMode, input.TLSVerify, input.RequestTimeoutSeconds,
		input.CheckIntervalSeconds, input.ActorID,
	)
	if err != nil {
		return nil, err
	}
	var credential *CredentialMaterial
	if input.Credential != nil {
		if !validAuthType(strings.TrimSpace(input.Credential.AuthType)) || strings.TrimSpace(input.Credential.Secret) == "" {
			return nil, ErrInvalidInstance
		}
		if strings.TrimSpace(input.Credential.AccessScope) != "" && !validAccessScope(strings.TrimSpace(input.Credential.AccessScope)) {
			return nil, ErrInvalidInstance
		}
		if input.Credential.ExpiresAt > 0 && input.Credential.ExpiresAt <= common.GetTimestamp() {
			return &PreflightResult{Success: false, Stages: pendingConnectionStages(), ErrorCode: ProbeErrorCredentialExpired, Advice: "Rotate the managed credential before saving the instance."}, nil
		}
		credential = &CredentialMaterial{
			AuthType: strings.TrimSpace(input.Credential.AuthType), Secret: input.Credential.Secret,
			UserID: strings.TrimSpace(input.Credential.UserID), AccessScope: normalizedAccessScope(input.Credential.AccessScope),
		}
	}
	policy, err := ConnectorPolicyFromEnvironment()
	if err != nil {
		return nil, err
	}
	connector, err := NewConnector(instance, policy)
	if err != nil {
		return nil, err
	}
	adapter, err := adapterForKind(instance.Kind)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now()
	probe, probeErr := adapter.Probe(ctx, connector, credential)
	if probeErr != nil {
		code := managedInstanceObservationErrorCode(probeErr)
		return &PreflightResult{
			Success: false, Stages: failedConnectionStages(probeErr), ErrorCode: code,
			Advice: preflightAdvice(code),
		}, nil
	}
	probe.LatencyMS = time.Since(startedAt).Milliseconds()
	probe.CheckedAt = common.GetTimestamp()
	return &PreflightResult{Success: true, Probe: probe, Stages: succeededConnectionStages()}, nil
}

func pendingConnectionStages() []ConnectionStage {
	return []ConnectionStage{
		{Name: "dns", Status: "not_run"}, {Name: "tcp", Status: "not_run"},
		{Name: "tls", Status: "not_run"}, {Name: "http", Status: "not_run"},
		{Name: "authentication", Status: "not_run"}, {Name: "capability", Status: "not_run"},
	}
}

func succeededConnectionStages() []ConnectionStage {
	stages := pendingConnectionStages()
	for index := range stages {
		stages[index].Status = "succeeded"
	}
	return stages
}

func failedConnectionStages(err error) []ConnectionStage {
	stages := pendingConnectionStages()
	failedIndex := 3
	var dnsError *net.DNSError
	var tlsError tls.RecordHeaderError
	var tlsVerificationError *tls.CertificateVerificationError
	var unknownAuthorityError x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var certificateInvalidError x509.CertificateInvalidError
	code := managedInstanceObservationErrorCode(err)
	switch {
	case errors.As(err, &dnsError):
		failedIndex = 0
	case errors.As(err, &tlsError), errors.As(err, &tlsVerificationError), errors.As(err, &unknownAuthorityError), errors.As(err, &hostnameError), errors.As(err, &certificateInvalidError):
		failedIndex = 2
	case code == ProbeErrorAuthentication, code == ProbeErrorPermission, code == ProbeErrorCredentialExpired, code == ProbeErrorTwoFactorRequired:
		failedIndex = 4
	case errors.Is(err, ErrUnsupportedCapability):
		failedIndex = 5
	default:
		var networkError net.Error
		if errors.As(err, &networkError) {
			failedIndex = 1
		}
	}
	for index := range stages {
		if index < failedIndex {
			stages[index].Status = "succeeded"
		} else if index == failedIndex {
			stages[index].Status = "failed"
		}
	}
	return stages
}

func preflightAdvice(code string) string {
	switch code {
	case ProbeErrorAuthentication, ProbeErrorCredentialExpired:
		return "Verify the account name and password."
	case ProbeErrorTwoFactorRequired:
		return "This account requires two-factor authentication. Use an account without 2FA or connect with an administrator token."
	case ProbeErrorPermission:
		return "Grant the management credential the required read permissions."
	case "target_blocked":
		return "Add the private target to the managed instance outbound allowlist after reviewing the network boundary."
	case ProbeErrorInvalidResponse:
		return "Confirm the selected instance type and supported remote version."
	case "tls_verification_failed":
		return "Install a publicly trusted TLS certificate on the target or configure a trusted internal certificate."
	case "tls_failed":
		return "Confirm that the URL scheme matches the target service protocol."
	case "dns_failed":
		return "Confirm that the target hostname resolves from the control-plane server."
	case "network_failed":
		return "Confirm that the control-plane server can reach the target host and port."
	default:
		return "Verify DNS, network reachability, TLS trust, and the configured base URL."
	}
}
