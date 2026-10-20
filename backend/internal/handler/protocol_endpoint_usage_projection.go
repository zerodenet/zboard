package handler

import "encoding/json"

// Daily projection lag is an explicit unknown, including for a new endpoint
// without any projected rows. Operational observations remain independent.
func (u protocolEndpointUsage) MarshalJSON() ([]byte, error) {
	type wire protocolEndpointUsage
	if !u.TrafficProjectionPending {
		return json.Marshal(wire(u))
	}
	return json.Marshal(struct {
		wire
		UsedBytesToday *int64 `json:"used_bytes_today"`
		UsedBytesTotal *int64 `json:"used_bytes_total"`
	}{wire: wire(u)})
}
