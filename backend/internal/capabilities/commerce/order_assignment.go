package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"unicode/utf8"
)

type OrderAssignmentRequest struct {
	PayableAmount        *int64 `json:"payable_amount"`
	UserID               uint   `json:"user_id"`
	PlanSKUID            uint   `json:"plan_sku_id"`
	TargetSubscriptionID uint   `json:"target_subscription_id"`
	Note                 string `json:"note"`
	RequestID            string `json:"request_id"`
}

type AssignedOrderInput struct {
	Request              OrderAssignmentRequest
	TradeNo, Fingerprint string
}
type OrderAssignmentRepository interface {
	Assign(context.Context, uint, AssignedOrderInput) (Order, error)
}
type OrderAssignment struct{ Repository OrderAssignmentRepository }

func (s OrderAssignment) Assign(ctx context.Context, actor uint, request OrderAssignmentRequest) (Order, error) {
	if actor == 0 {
		return Order{}, ErrOrderPermission
	}
	request.Note = strings.TrimSpace(request.Note)
	requestID, requestIDError := uuid.Parse(request.RequestID)
	fields := map[string]string{}
	if request.UserID == 0 {
		fields["user_id"] = "请选择用户。"
	}
	if request.PlanSKUID == 0 {
		fields["plan_sku_id"] = "请选择销售规格。"
	}
	if request.PayableAmount != nil && (*request.PayableAmount < 0 || *request.PayableAmount > 9007199254740991) {
		fields["payable_amount"] = "应付金额必须为不小于 0 的整数分，且不能超过安全整数范围。"
	}
	if request.Note == "" || utf8.RuneCountInString(request.Note) > 500 {
		fields["note"] = "请填写分配原因，最多 500 字。"
	}
	if requestIDError != nil || requestID == uuid.Nil {
		fields["request_id"] = "缺少有效的操作标识，请重新打开分配窗口。"
	}
	if len(fields) != 0 {
		return Order{}, validationError("订单分配失败。", fields)
	}
	request.RequestID = requestID.String()
	payload, err := json.Marshal(request)
	if err != nil {
		return Order{}, err
	}
	in := AssignedOrderInput{Request: request, Fingerprint: fmt.Sprintf("%x", sha256.Sum256(payload)), TradeNo: fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("admin-assign:%d:%s", actor, request.RequestID))))}
	return s.Repository.Assign(ctx, actor, in)
}
