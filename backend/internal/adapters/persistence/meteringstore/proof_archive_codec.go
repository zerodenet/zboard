package meteringstore

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

const proofArchiveBodyLimit = 65535
const proofArchiveReadBudget = 1024 * 1024
const proofArchiveHeaderAllowance = 256
const proofArchiveMinimumSaving = 64
const proofArchiveMarkerType = "metering_native_proof_archive"

var errProofArchiveBudget = errors.New("native proof material byte budget exhausted")

type proofArchiveReference struct {
	Type    string `json:"type"`
	Version uint64 `json:"version"`
	SHA256  string `json:"sha256"`
}

func archiveReference(digest string) string {
	b, _ := json.Marshal(proofArchiveReference{proofArchiveMarkerType, 1, digest})
	return string(b)
}

func isArchiveReference(meta string) bool {
	var discriminator struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(meta), &discriminator) == nil {
		return discriminator.Type == proofArchiveMarkerType
	}
	return strings.HasPrefix(strings.TrimSpace(meta), `{"type":"`+proofArchiveMarkerType+`"`)
}

func parseArchiveReference(meta string) (proofArchiveReference, error) {
	var ref proofArchiveReference
	d := json.NewDecoder(strings.NewReader(meta))
	d.DisallowUnknownFields()
	if len(meta) > 256 || d.Decode(&ref) != nil || ref.Type != proofArchiveMarkerType || ref.Version != 1 || len(ref.SHA256) != 64 {
		return ref, metering.ErrProofArchive
	}
	if _, err := hex.DecodeString(ref.SHA256); err != nil {
		return ref, metering.ErrProofArchive
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ref, metering.ErrProofArchive
	}
	if meta != archiveReference(ref.SHA256) {
		return ref, metering.ErrProofArchive
	}
	return ref, nil
}

// Include hidden nonce/epoch and normalize times, rather than hashing the
// public JSON projection which intentionally omits financial identities.
func proofFactDigest(record model.TrafficRecord) string {
	return metering.ProofFactDigest(metering.TrafficRecord(record))
}

func proofBodyDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Only native proof formats produced by the current accounting writers can be
// sealed. Optional legacy diagnostics retain their original TTL/cap policy;
// unknown historical JSON remains untouched for classification.
func knownNativeProof(record model.TrafficRecord, body string) bool {
	if record.ID == 0 || record.NodeID == 0 || record.SubscriptionID == 0 || record.ProtocolEndpointID == 0 || record.ReportID == "" || record.FlowID == "" || record.Nonce != completionNonce(record.ReportID) || (record.EventType != "flow.updated" && record.EventType != "flow.completed") || record.RawBytes < 0 || record.UsedBytes < 0 || record.UploadBytes < 0 || record.DownloadBytes < 0 || record.ProtocolMultiplierMilli < 0 {
		return false
	}
	var proof struct {
		SourceID   string  `json:"source_id"`
		CoreID     string  `json:"core_instance_id"`
		Sequence   uint64  `json:"sequence"`
		Credential string  `json:"credential_id"`
		Buffered   *bool   `json:"buffered"`
		Continued  *bool   `json:"continued_flow"`
		Samples    *uint64 `json:"accounting_samples"`
		First      *uint64 `json:"first_sequence"`
		Rounding   *uint64 `json:"rounding_numerator"`
	}
	d := json.NewDecoder(strings.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(&proof) != nil || d.Decode(new(any)) != io.EOF {
		return false
	}
	if proof.SourceID == "" || proof.CoreID == "" || proof.Credential == "" || proof.Sequence == 0 || len(proof.SourceID) > 191 || len(proof.CoreID) > 191 || len(proof.Credential) > 191 {
		return false
	}
	if proof.Buffered != nil {
		return *proof.Buffered && proof.Continued == nil && proof.Samples != nil && *proof.Samples > 0 && *proof.Samples <= 2000 && proof.First != nil && *proof.First > 0 && *proof.First <= proof.Sequence && proof.Rounding != nil && *proof.Rounding <= *proof.Samples*999
	}
	return record.EventType == "flow.completed" && proof.Continued != nil && proof.Samples == nil && proof.First == nil && proof.Rounding == nil
}

func compressProof(body string) ([]byte, error) {
	var out bytes.Buffer
	w, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err = w.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func decodeProof(header model.TrafficMetaArchive, payload []byte) (string, error) {
	bad := func() (string, error) { return "", metering.ErrProofArchive }
	if header.Version != 1 || header.TrafficRecordID == 0 || header.SourceBytes == 0 || header.SourceBytes > proofArchiveBodyLimit || header.CompressedBytes == 0 || header.CompressedBytes > proofArchiveBodyLimit || uint64(len(payload)) != header.CompressedBytes || len(header.SourceSHA256) != 64 || len(header.FactSHA256) != 64 || header.CreatedAt.IsZero() {
		return bad()
	}
	raw := bytes.NewReader(payload)
	r, err := gzip.NewReader(raw)
	if err != nil {
		return bad()
	}
	r.Multistream(false)
	body, err := io.ReadAll(io.LimitReader(r, int64(header.SourceBytes)+1))
	closed := r.Close()
	if err != nil || closed != nil || uint64(len(body)) != header.SourceBytes || raw.Len() != 0 || proofBodyDigest(string(body)) != header.SourceSHA256 {
		return bad()
	}
	return string(body), nil
}

func findProofArchiveHeader(tx *gorm.DB, record model.TrafficRecord) (model.TrafficMetaArchive, bool, error) {
	var header model.TrafficMetaArchive
	var material struct {
		model.TrafficMetaArchive `gorm:"embedded"`
		PayloadBytes             uint64
	}
	length := "octet_length(payload)"
	if err := tx.Model(&model.TrafficMetaArchive{}).Select("traffic_record_id,version,source_bytes,substr(source_sha256,1,65) AS source_sha256,substr(fact_sha256,1,65) AS fact_sha256,compressed_bytes,created_at,"+length+" AS payload_bytes").Where("traffic_record_id = ?", record.ID).Take(&material).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return header, false, nil
		}
		return header, false, errors.Join(metering.ErrProofArchive, err)
	}
	header = material.TrafficMetaArchive
	if header.Version != 1 || header.TrafficRecordID != record.ID || header.SourceBytes == 0 || header.CompressedBytes == 0 || header.CompressedBytes > proofArchiveBodyLimit || header.SourceBytes > proofArchiveBodyLimit || len(header.SourceSHA256) != 64 || len(header.FactSHA256) != 64 || header.CreatedAt.IsZero() || header.FactSHA256 != proofFactDigest(record) {
		return header, true, metering.ErrProofArchive
	}
	return header, true, nil
}

func readProofArchive(tx *gorm.DB, record model.TrafficRecord, budgets ...*int) (model.TrafficMetaArchive, string, error) {
	header, found, err := findProofArchiveHeader(tx, record)
	if err != nil || !found {
		return header, "", errors.Join(metering.ErrProofArchive, err)
	}
	return readProofArchivePayload(tx, header, budgets...)
}

func readProofArchivePayload(tx *gorm.DB, header model.TrafficMetaArchive, budgets ...*int) (model.TrafficMetaArchive, string, error) {
	var size uint64
	if err := tx.Model(&model.TrafficMetaArchive{}).Select("octet_length(payload)").Where("traffic_record_id = ?", header.TrafficRecordID).Scan(&size).Error; err != nil {
		return header, "", errors.Join(metering.ErrProofArchive, err)
	}
	if size != header.CompressedBytes {
		return header, "", metering.ErrProofArchive
	}
	if len(budgets) > 0 && budgets[0] != nil {
		cost := int(header.CompressedBytes+header.SourceBytes) + 2
		if *budgets[0] < cost {
			return header, "", errProofArchiveBudget
		}
		*budgets[0] -= cost
	}
	var body struct{ Payload []byte }
	if err := tx.Model(&model.TrafficMetaArchive{}).Select("substr(payload,1,?) AS payload", int(header.CompressedBytes)+1).Where("traffic_record_id = ?", header.TrafficRecordID).Take(&body).Error; err != nil {
		return header, "", errors.Join(metering.ErrProofArchive, err)
	}
	decoded, err := decodeProof(header, body.Payload)
	return header, decoded, err
}

func hydrateProof(tx *gorm.DB, record model.TrafficRecord, budgets ...*int) (model.TrafficRecord, error) {
	// Legacy Meta is arbitrary optional diagnostic text, including user-chosen
	// type keys. It cannot become an internal reference merely by naming one.
	if record.EventType == "" {
		return record, nil
	}
	header, found, err := findProofArchiveHeader(tx, record)
	if err != nil {
		return record, err
	}
	if !found {
		if isArchiveReference(record.Meta) {
			return record, metering.ErrProofArchive
		}
		return record, nil
	}
	// Restored inline proof is complete without decoding its retained copy.
	if uint64(len(record.Meta)) == header.SourceBytes && proofBodyDigest(record.Meta) == header.SourceSHA256 {
		return record, nil
	}
	if record.Meta != archiveReference(header.SourceSHA256) {
		return record, metering.ErrProofArchive
	}
	header, body, err := readProofArchivePayload(tx, header, budgets...)
	if err != nil {
		return record, errors.Join(metering.ErrProofArchive, err)
	}
	record.Meta = body
	return record, nil
}
