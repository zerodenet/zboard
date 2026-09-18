package metering

import (
	"fmt"
	"strings"
)

func RiskState(score int, policy Policy) string {
	switch {
	case score >= policy.ViolationScore:
		return "violated"
	case score >= policy.WarningScore:
		return "suspected"
	default:
		return "normal"
	}
}

func ScoreTransition(policy Policy, score int, connectionStarts, workingNodes int64) (int, string, string) {
	penalty := 0
	reasons := make([]string, 0, 2)
	if connectionStarts > int64(policy.ConnectionStartThreshold) {
		penalty += policy.ConnectionStartPenalty
		reasons = append(reasons, fmt.Sprintf("connection_starts=%d>%d:+%d", connectionStarts, policy.ConnectionStartThreshold, policy.ConnectionStartPenalty))
	}
	if workingNodes > int64(policy.WorkingNodeThreshold) {
		penalty += policy.WorkingNodePenalty
		reasons = append(reasons, fmt.Sprintf("working_nodes=%d>%d:+%d", workingNodes, policy.WorkingNodeThreshold, policy.WorkingNodePenalty))
	}
	if penalty == 0 {
		next := score - policy.RecoveryPerInterval
		if next < 0 {
			next = 0
		}
		return next, RiskState(next, policy), fmt.Sprintf("normal interval: recovery -%d", policy.RecoveryPerInterval)
	}
	next := score + penalty
	if next > policy.ScoreMax {
		next = policy.ScoreMax
	}
	return next, RiskState(next, policy), strings.Join(reasons, "; ")
}
