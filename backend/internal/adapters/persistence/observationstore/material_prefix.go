package observationstore

import (
	"encoding/binary"
	"encoding/hex"

	"github.com/zerodenet/zboard/backend/internal/model"
)

// verifyObservationPrefix checks only the fixed version-one source prefix. It
// never decodes a potentially huge historical variable field or allocates its
// length. Remaining material still requires the resumable full SHA256 check.
func verifyObservationPrefix(h model.NodeObservationIntent, part []byte) error {
	if len(part) < 24 || binary.BigEndian.Uint64(part[:8]) != 1 {
		return invalid("invalid observation source identity prefix")
	}
	if binary.BigEndian.Uint64(part[8:16]) != uint64(h.NodeID) {
		return invalid("observation node identity prefix mismatch")
	}
	length := binary.BigEndian.Uint64(part[16:24])
	if h.MaterialBytes < 24 || length > h.MaterialBytes-24 {
		return invalid("invalid observation asset identity length")
	}
	// Enqueue normalizes any non64hex label to unknown. Match that exact rule,
	// retaining case for a valid label, without reading a long legacy label.
	asset := ""
	if length == 64 {
		if len(part) < 88 {
			return invalid("incomplete observation asset identity prefix")
		}
		value := string(part[24:88])
		if _, err := hex.DecodeString(value); err == nil {
			asset = value
		}
	}
	if asset != h.AssetTag {
		return invalid("observation asset identity prefix mismatch")
	}
	return nil
}
