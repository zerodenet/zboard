package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

const NodeRetirementHandler = "node_retirement"
const NodeRetirementRevision = "1"

type NodeRetirementRunInput struct {
	Revision string `json:"revision"`
	NodeID   uint   `json:"node_id"`
}

// RemoteCleanupUnconfirmed records a host-side result without turning missing
// SSH access into a successful native cleanup or blocking provider deletion.
type RemoteCleanupUnconfirmed struct{ Reason string }

func (e *RemoteCleanupUnconfirmed) Error() string { return e.Reason }

type RetirementBlocked struct{ Reason string }

func (e *RetirementBlocked) Error() string { return e.Reason }

type RetirementStep struct {
	Kind              string `json:"kind"`
	ProviderAccountID uint   `json:"provider_account_id,omitempty"`
	ProviderZoneID    string `json:"provider_zone_id,omitempty"`
	ProviderRecordID  string `json:"provider_record_id,omitempty"`
	ID                uint   `json:"id"`
	Label             string `json:"label"`
	State             string `json:"state"`
	Message           string `json:"message,omitempty"`
}
type Retirement struct {
	NodeID    uint             `json:"node_id"`
	NodeName  string           `json:"node_name"`
	Mode      string           `json:"mode"`
	State     string           `json:"state"`
	RunID     string           `json:"run_id"`
	Attempt   int              `json:"attempt"`
	Steps     []RetirementStep `json:"steps"`
	LastError string           `json:"last_error,omitempty"`
}
type RetirementRepository interface {
	Start(context.Context, uint, uint, string) (Retirement, error)
	Read(context.Context, uint) (Retirement, error)
	Advance(context.Context, uint, string, int, string, string) error
	Finalize(context.Context, uint, string) error
	Material(context.Context, uint, string, RetirementStep) (RetirementMaterial, error)
}

// Execution-only facts. SSH ciphertext is never serialized into progress APIs.
type RetirementMaterial struct {
	SSH                   NodeCleanupSnapshot `json:"-"`
	DNS                   ManagedDNSRecord
	CertificateID         uint
	CertPath              string
	KeyPath               string
	Shared                bool
	Mode                  string
	RemoteCleanupPossible bool
	RemoteCleanupReason   string
}
type RetirementRemote interface {
	RetireResource(context.Context, uint, string, RetirementStep) (string, error)
}
type NodeRetirement struct {
	Repository RetirementRepository
	Remote     RetirementRemote
}

func (s NodeRetirement) Execute(ctx context.Context, run jobs.Run) error {
	var input NodeRetirementRunInput
	if run.Owner != "system" || run.Handler != NodeRetirementHandler || json.Unmarshal([]byte(run.Payload), &input) != nil || input.Revision != NodeRetirementRevision || input.NodeID == 0 {
		return jobs.ErrInvalid
	}
	item, err := s.Repository.Read(ctx, input.NodeID)
	if err != nil {
		return err
	}
	if item.RunID != run.ID {
		return jobs.ErrLeaseLost
	}
	for i, step := range item.Steps {
		if step.State == "succeeded" || step.State == "retained" || (item.Mode == "delete" && step.State == "unconfirmed") {
			continue
		}
		if err := s.Repository.Advance(ctx, item.NodeID, run.ID, i, "running", ""); err != nil {
			return err
		}
		message, err := s.Remote.RetireResource(ctx, item.NodeID, run.ID, step)
		state := "succeeded"
		if message != "" && item.Mode != "delete" {
			state = "retained"
		}
		var unavailable *RemoteCleanupUnconfirmed
		if item.Mode == "delete" && (step.Kind == "certificate" || step.Kind == "zero") && errors.As(err, &unavailable) {
			state, message, err = "unconfirmed", unavailable.Reason, nil
		}
		if err != nil {
			// Persist a bounded public explanation, never raw provider/SSH errors.
			message = fmt.Sprintf("%s删除未完成；请检查供应商权限和资源归属后重试。", step.Label)
			var blocked *RetirementBlocked
			if errors.As(err, &blocked) {
				message = blocked.Reason
			}
			failureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = s.Repository.Advance(failureCtx, item.NodeID, run.ID, i, "failed", message)
			cancel()
			return errors.New(message)
		}
		if err := s.Repository.Advance(ctx, item.NodeID, run.ID, i, state, message); err != nil {
			return err
		}
	}
	return s.Repository.Finalize(ctx, item.NodeID, run.ID)
}
