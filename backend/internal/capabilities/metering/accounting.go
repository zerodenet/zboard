package metering

import (
	"errors"
	"math"
	"math/big"
)

func TrafficBytesForMode(up, down int64, mode int16) int64 {
	switch mode {
	case 1:
		return up
	case 2:
		return down
	default:
		return up + down
	}
}
func CheckedTrafficBytes(up, down int64, mode int16) (int64, error) {
	if up < 0 || down < 0 {
		return 0, errors.New("negative cumulative traffic")
	}
	if mode != 1 && mode != 2 && up > math.MaxInt64-down {
		return 0, errors.New("cumulative traffic exceeds supported range")
	}
	return TrafficBytesForMode(up, down, mode), nil
}
func BilledTrafficBytes(raw, multiplier int64) (int64, error) {
	if raw <= 0 || multiplier <= 0 {
		return 0, nil
	}
	value := big.NewInt(raw)
	value.Mul(value, big.NewInt(multiplier))
	value.Add(value, big.NewInt(999))
	value.Div(value, big.NewInt(1000))
	if !value.IsInt64() {
		return 0, errors.New("calculated traffic exceeds supported range")
	}
	return value.Int64(), nil
}

type FlowCounters struct{ Raw, Upload, Download int64 }
type ChargeDelta struct {
	FlowCounters
	Charged int64
}

func CalculateCharge(current, previous FlowCounters, multiplier, total, used int64) (ChargeDelta, error) {
	if current.Raw < 0 || current.Upload < 0 || current.Download < 0 || previous.Raw < 0 || previous.Upload < 0 || previous.Download < 0 || current.Raw < previous.Raw || current.Upload < previous.Upload || current.Download < previous.Download {
		return ChargeDelta{}, errors.New("flow cumulative counters cannot produce a negative delta")
	}
	out := ChargeDelta{FlowCounters: FlowCounters{Raw: current.Raw - previous.Raw, Upload: current.Upload - previous.Upload, Download: current.Download - previous.Download}}
	billed, err := BilledTrafficBytes(out.Raw, multiplier)
	if err != nil {
		return ChargeDelta{}, err
	}
	remaining := int64(0)
	if total > used && used >= 0 {
		remaining = total - used
	}
	out.Charged = billed
	if out.Charged > remaining {
		out.Charged = remaining
	}
	return out, nil
}
