package meteringstore

import (
	"crypto/sha256"
	"fmt"
)

// Match the published MySQL TEXT byte capacity on both supported databases.
// This optional reporter diagnostic is never an accounting identity, amount,
// cumulative cursor or rounding proof. Native accounting evidence is separate.
const nodeReportMetadataByteLimit = 65535

func boundedNodeReportMetadata(meta string) string {
	if len(meta) <= nodeReportMetadataByteLimit {
		return meta
	}
	digest := sha256.Sum256([]byte(meta))
	return fmt.Sprintf(`{"type":"diagnostic_meta_omitted","version":1,"reason":"text_byte_limit","original_bytes":%d,"max_bytes":%d,"sha256":"%x"}`, len(meta), nodeReportMetadataByteLimit, digest)
}
