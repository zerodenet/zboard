package network

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrProtocolEndpointOrderPermission      = errors.New("protocol endpoint order requires administrator")
	ErrProtocolEndpointOrderVersionRequired = errors.New("protocol endpoint order version required")
	ErrProtocolEndpointOrderConflict        = errors.New("protocol endpoint order changed")
)

type ProtocolEndpointOrderValidation struct {
	Fields map[string]string
}

func (e *ProtocolEndpointOrderValidation) Error() string { return "协议交付顺序校验失败。" }

type ProtocolEndpointOrderItem struct {
	ID        uint   `json:"id"`
	NodeID    uint   `json:"node_id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	IsActive  bool   `json:"is_active"`
	SortOrder int    `json:"sort_order"`
}

type ProtocolEndpointOrderSnapshot struct {
	Items   []ProtocolEndpointOrderItem `json:"items"`
	Version string                      `json:"version"`
	Total   int                         `json:"total"`
}

type ProtocolEndpointOrderRequest struct {
	OrderedIDs      []uint `json:"ordered_ids"`
	ExpectedVersion string `json:"expected_version"`
}

type ProtocolEndpointOrderRepository interface {
	ReadProtocolEndpointOrder(context.Context, uint) (ProtocolEndpointOrderSnapshot, error)
	UpdateProtocolEndpointOrder(context.Context, uint, ProtocolEndpointOrderRequest) (ProtocolEndpointOrderSnapshot, bool, error)
}

type ProtocolEndpointOrder struct {
	Repository ProtocolEndpointOrderRepository
}

func (s ProtocolEndpointOrder) Read(ctx context.Context, actor uint) (ProtocolEndpointOrderSnapshot, error) {
	if actor == 0 {
		return ProtocolEndpointOrderSnapshot{}, ErrProtocolEndpointOrderPermission
	}
	return s.Repository.ReadProtocolEndpointOrder(ctx, actor)
}

func (s ProtocolEndpointOrder) Update(ctx context.Context, actor uint, request ProtocolEndpointOrderRequest) (ProtocolEndpointOrderSnapshot, bool, error) {
	if actor == 0 {
		return ProtocolEndpointOrderSnapshot{}, false, ErrProtocolEndpointOrderPermission
	}
	request.ExpectedVersion = strings.TrimSpace(request.ExpectedVersion)
	if request.ExpectedVersion == "" {
		return ProtocolEndpointOrderSnapshot{}, false, ErrProtocolEndpointOrderVersionRequired
	}
	if duplicate, invalid := DuplicateOrZeroID(request.OrderedIDs); invalid {
		return ProtocolEndpointOrderSnapshot{}, false, &ProtocolEndpointOrderValidation{Fields: map[string]string{
			"ordered_ids": fmt.Sprintf("协议服务 ID #%d 无效或重复，请重新加载完整列表。", duplicate),
		}}
	}
	return s.Repository.UpdateProtocolEndpointOrder(ctx, actor, request)
}

func ProtocolEndpointOrderVersion(items []ProtocolEndpointOrderItem) string {
	ordered := append([]ProtocolEndpointOrderItem(nil), items...)
	sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].ID < ordered[right].ID })
	digest := sha256.New()
	for _, item := range ordered {
		_, _ = digest.Write([]byte(strconv.FormatUint(uint64(item.ID), 10)))
		_, _ = digest.Write([]byte(":"))
		_, _ = digest.Write([]byte(strconv.Itoa(item.SortOrder)))
		_, _ = digest.Write([]byte(";"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func ValidateCompleteProtocolEndpointOrder(items []ProtocolEndpointOrderItem, orderedIDs []uint) error {
	if len(items) != len(orderedIDs) {
		return &ProtocolEndpointOrderValidation{Fields: map[string]string{
			"ordered_ids": fmt.Sprintf("必须提交全部 %d 个协议服务，当前仅收到 %d 个。", len(items), len(orderedIDs)),
		}}
	}
	available := make(map[uint]struct{}, len(items))
	for _, item := range items {
		available[item.ID] = struct{}{}
	}
	for _, endpointID := range orderedIDs {
		if _, exists := available[endpointID]; !exists {
			return &ProtocolEndpointOrderValidation{Fields: map[string]string{
				"ordered_ids": fmt.Sprintf("协议服务 #%d 不在当前完整范围内，请重新加载。", endpointID),
			}}
		}
	}
	return nil
}

func DuplicateOrZeroID(values []uint) (uint, bool) {
	seen := make(map[uint]struct{}, len(values))
	for _, value := range values {
		if value == 0 {
			return value, true
		}
		if _, exists := seen[value]; exists {
			return value, true
		}
		seen[value] = struct{}{}
	}
	return 0, false
}
