package metering

import "context"

const EvaluationJob = "fair_use_evaluation"

type EvaluationRequest struct {
	Revision       string `json:"revision"`
	Actor          uint   `json:"actor"`
	SubscriptionID uint   `json:"subscription_id"`
}
type EvaluationReceipt struct {
	RunID          string `json:"run_id"`
	State          string `json:"state"`
	SubscriptionID uint   `json:"subscription_id"`
}
type EvaluationRequestRepository interface {
	Enqueue(context.Context, uint, uint) (EvaluationReceipt, error)
}
type EvaluationRequests struct{ Repository EvaluationRequestRepository }

func (s EvaluationRequests) Enqueue(ctx context.Context, actor, subscription uint) (EvaluationReceipt, error) {
	if err := validateScope(actor, PolicyScope{Type: "subscription", ID: subscription}); err != nil {
		return EvaluationReceipt{}, err
	}
	return s.Repository.Enqueue(ctx, actor, subscription)
}
