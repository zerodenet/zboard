package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
)

type MigrationExecutionRepository interface {
	// Begin binds the task to a still-live, claimed public Run before mutating it.
	Begin(context.Context, uint, jobs.Run) (string, error)
	Authorize(context.Context, uint) error
	Finish(context.Context, uint, error) error
}
type MigrationDecryptor interface{ Decrypt(string) (string, error) }
type MigrationCopier interface {
	Copy(context.Context, MigrationTarget, uint, string) error
}

type MigrationExecution struct {
	Repository MigrationExecutionRepository
	Cipher     MigrationDecryptor
	Copier     MigrationCopier
}

func (s MigrationExecution) Execute(ctx context.Context, run jobs.Run) (err error) {
	var input struct {
		Revision string `json:"revision"`
		TaskID   uint   `json:"task_id"`
		ActorID  uint   `json:"actor_id"`
	}
	if json.Unmarshal([]byte(run.Payload), &input) != nil || input.Revision != "1" || input.TaskID == 0 || input.ActorID == 0 || run.ID == "" || run.Owner != "system" || run.Handler != "database_migration" || run.Resource != jobs.MaintenanceResource || run.Key != fmt.Sprintf("database_migration:%d", input.TaskID) {
		return jobs.ErrInvalid
	}
	payload, err := s.Repository.Begin(ctx, input.TaskID, run)
	if err != nil {
		return err
	}
	defer func() {
		if recover() != nil {
			err = errors.Join(err, jobs.ErrUncertain, errors.New("database migration panicked; inspect destination"))
		}
		finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if finishErr := s.Repository.Finish(finishCtx, input.TaskID, err); finishErr != nil {
			err = errors.Join(err, finishErr, jobs.ErrUncertain)
		}
	}()
	if err := s.Repository.Authorize(ctx, input.ActorID); err != nil {
		return err
	}
	ciphertext, err := DecodeMigrationSecret(payload)
	if err != nil {
		return err
	}
	plaintext, err := s.Cipher.Decrypt(ciphertext)
	if err != nil {
		return err
	}
	var target MigrationTarget
	if err := json.Unmarshal([]byte(plaintext), &target); err != nil {
		return err
	}
	if err := s.Copier.Copy(ctx, target, input.TaskID, run.ID); err != nil {
		return errors.Join(err, jobs.ErrUncertain)
	}
	return nil
}
