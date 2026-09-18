package network

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrDeliveryPermission = errors.New("delivery order requires administrator")
var ErrDeliveryVersionRequired = errors.New("delivery order version required")
var ErrDeliveryConflict = errors.New("delivery order changed")

type DeliveryValidation struct{ Message string }

func (e *DeliveryValidation) Error() string { return e.Message }

type DeliveryEndpoint struct {
	ID        uint   `json:"id"`
	NodeID    uint   `json:"node_id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	IsActive  bool   `json:"is_active"`
	SortOrder int    `json:"sort_order"`
}
type DeliveryItem struct {
	DeliveryEndpoint
	Key         string `json:"key"`
	ServiceKind string `json:"service_kind"`
}
type DeliverySnapshot struct {
	Items   []DeliveryItem `json:"items"`
	Version string         `json:"version"`
	Total   int            `json:"total"`
}
type DeliveryOrderRequest struct {
	OrderedKeys     []string `json:"ordered_keys"`
	ExpectedVersion string   `json:"expected_version"`
}
type DeliveryOrderRepository interface {
	Read(context.Context, uint) (DeliverySnapshot, error)
	Update(context.Context, uint, DeliveryOrderRequest) (DeliverySnapshot, error)
}
type DeliveryOrder struct{ Repository DeliveryOrderRepository }

func (s DeliveryOrder) Read(ctx context.Context, actor uint) (DeliverySnapshot, error) {
	if actor == 0 {
		return DeliverySnapshot{}, ErrDeliveryPermission
	}
	return s.Repository.Read(ctx, actor)
}
func (s DeliveryOrder) Update(ctx context.Context, actor uint, request DeliveryOrderRequest) (DeliverySnapshot, error) {
	if actor == 0 {
		return DeliverySnapshot{}, ErrDeliveryPermission
	}
	if strings.TrimSpace(request.ExpectedVersion) == "" {
		return DeliverySnapshot{}, ErrDeliveryVersionRequired
	}
	return s.Repository.Update(ctx, actor, request)
}
func DeliveryKey(endpoint, entry uint) string {
	if entry != 0 {
		return fmt.Sprintf("entry:%d", entry)
	}
	return fmt.Sprintf("protocol:%d", endpoint)
}
func ValidateDeliveryOrder(snapshot DeliverySnapshot, request DeliveryOrderRequest) error {
	if snapshot.Version != request.ExpectedVersion {
		return ErrDeliveryConflict
	}
	available := map[string]bool{}
	for _, item := range snapshot.Items {
		available[item.Key] = true
	}
	if len(request.OrderedKeys) != len(available) {
		return &DeliveryValidation{Message: "请提交全部直连服务和前置入口。"}
	}
	for _, key := range request.OrderedKeys {
		if !available[key] {
			return &DeliveryValidation{Message: "列表包含重复或不存在的服务，请重新加载。"}
		}
		delete(available, key)
	}
	return nil
}
func ProjectDeliveryOrder(input []DeliveryItem) DeliverySnapshot {
	items := append([]DeliveryItem{}, input...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		if items[i].ServiceKind != items[j].ServiceKind {
			return items[i].ServiceKind == "listener"
		}
		return items[i].ID < items[j].ID
	})
	digest := sha256.New()
	for _, item := range items {
		fmt.Fprintf(digest, "%s:%d;", item.Key, item.SortOrder)
	}
	return DeliverySnapshot{Items: items, Version: hex.EncodeToString(digest.Sum(nil)), Total: len(items)}
}
