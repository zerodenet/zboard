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

type RetirementBlocked struct{ Reason string }

func (e *RetirementBlocked) Error() string { return e.Reason }

type RetirementStep struct {
	Kind    string `json:"kind"`
	ID      uint   `json:"id"`
	Label   string `json:"label"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
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
	SSH           NodeCleanupSnapshot `json:"-"`
	DNS           ManagedDNSRecord
	CertificateID uint
	CertPath      string
	KeyPath       string
	Shared        bool
}
type RetirementRemote interface {
	RetireResource(context.Context, uint, string, RetirementStep) (string, error)
}
type NodeRetirement struct {
	Repository RetirementRepository
	Remote     RetirementRemote
}

func (s NodeRetirement) Execute(ctx context.Context, run jobs.Run) error {
	var input struct {
		NodeID uint `json:"node_id"`
	}
	if run.Owner != "system" || run.Handler != NodeRetirementHandler || json.Unmarshal([]byte(run.Payload), &input) != nil || input.NodeID == 0 {
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
		if step.State == "succeeded" || step.State == "retained" {
			continue
		}
		if err := s.Repository.Advance(ctx, item.NodeID, run.ID, i, "running", ""); err != nil {
			return err
		}
		message, err := s.Remote.RetireResource(ctx, item.NodeID, run.ID, step)
		state := "succeeded"
		if message != "" {
			state = "retained"
		}
		if err != nil {
			// Persist a bounded public explanation, never raw provider/SSH errors.
			message = fmt.Sprintf("%s未完成；记录已保留，请检查连接、权限和资源归属后重试。", step.Label)
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
