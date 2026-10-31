package identity

import (
	"context"
	"errors"
	"strconv"
	"time"
)

const CleanupDaysKey = "user_cleanup_inactive_days"

type DeletionPreview struct {
	UserID          uint     `json:"user_id"`
	Allowed         bool     `json:"allowed"`
	Blockers        []string `json:"blockers"`
	HistoryRetained bool     `json:"history_retained"`
}
type DeletionBlocked struct{ Preview DeletionPreview }

func (e *DeletionBlocked) Error() string {
	return "账号当前不能删除，请先处理删除条件。"
}

type DeletionRepository interface {
	Inspect(context.Context, uint, uint, time.Time) (DeletionPreview, error)
	Remove(context.Context, uint, uint, time.Time) error
	Cleanup(context.Context, time.Time, int) (int, error)
	CleanupEnabled(context.Context) (bool, error)
}
type Deletion struct{ Repository DeletionRepository }

func (s Deletion) Preview(ctx context.Context, actor, id uint) (DeletionPreview, error) {
	if actor == 0 {
		return DeletionPreview{}, ErrPermission
	}
	if id == 0 {
		return DeletionPreview{}, ErrAccountNotFound
	}
	return s.Repository.Inspect(ctx, actor, id, time.Now().UTC())
}
func (s Deletion) Delete(ctx context.Context, actor, id uint) error {
	if actor == 0 {
		return ErrPermission
	}
	if id == 0 {
		return ErrAccountNotFound
	}
	return s.Repository.Remove(ctx, actor, id, time.Now().UTC())
}
func ParseCleanupDays(value string) (int, error) {
	days, err := strconv.Atoi(value)
	if err != nil || days < 0 || days > 36500 {
		return 0, errors.New("自动清理天数必须是0至36500的整数，0表示关闭。")
	}
	return days, nil
}
