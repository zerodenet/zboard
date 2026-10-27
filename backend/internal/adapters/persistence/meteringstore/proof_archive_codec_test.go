package meteringstore

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func codecProofHeader(t *testing.T, body string) (model.TrafficMetaArchive, []byte) {
	t.Helper()
	packed, err := compressProof(body)
	if err != nil {
		t.Fatal(err)
	}
	return model.TrafficMetaArchive{TrafficRecordID: 1, Version: 1, SourceBytes: uint64(len(body)), SourceSHA256: proofBodyDigest(body), FactSHA256: strings.Repeat("a", 64), CompressedBytes: uint64(len(packed)), CreatedAt: time.Now().UTC()}, packed
}

func TestNativeProofArchiveCodecPreservesExactBytesAndRejectsCorruption(t *testing.T) {
	body := strings.Repeat("native financial proof\x00汉字 ", 1000)
	header, packed := codecProofHeader(t, body)
	decoded, err := decodeProof(header, packed)
	if err != nil || decoded != body {
		t.Fatal("lossless proof", err)
	}
	for _, kind := range []string{"digest", "truncated", "trailing", "concatenated", "wrong_length", "oversized", "bomb"} {
		t.Run(kind, func(t *testing.T) {
			h, p := header, append([]byte{}, packed...)
			switch kind {
			case "digest":
				h.SourceSHA256 = strings.Repeat("0", 64)
			case "truncated":
				p = p[:len(p)-1]
				h.CompressedBytes = uint64(len(p))
			case "trailing":
				p = append(p, 1)
				h.CompressedBytes = uint64(len(p))
			case "concatenated":
				p = append(p, packed...)
				h.CompressedBytes = uint64(len(p))
			case "wrong_length":
				h.SourceBytes--
			case "oversized":
				h.SourceBytes = proofArchiveBodyLimit + 1
			case "bomb":
				var b bytes.Buffer
				w := gzip.NewWriter(&b)
				_, _ = w.Write(bytes.Repeat([]byte("z"), 4*proofArchiveBodyLimit))
				_ = w.Close()
				p = b.Bytes()
				h.CompressedBytes = uint64(len(p))
				h.SourceBytes = 32
			}
			if _, err := decodeProof(h, p); err == nil {
				t.Fatal("unverified proof accepted")
			}
		})
	}
}

func TestNativeProofArchiveFactDigestIncludesHiddenIdentityAndExcludesOnlyMaterial(t *testing.T) {
	r := model.TrafficRecord{ID: 1, NodeID: 2, SubscriptionID: 3, Nonce: "hidden", EntitlementEpoch: 4, UsedBytes: 5, Meta: "original", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	before := proofFactDigest(r)
	r.Meta = "archived"
	if proofFactDigest(r) != before {
		t.Fatal("material changed financial binding")
	}
	r.Nonce = "different"
	if proofFactDigest(r) == before {
		t.Fatal("hidden nonce omitted")
	}
	r.Nonce = "hidden"
	r.EntitlementEpoch++
	if proofFactDigest(r) == before {
		t.Fatal("hidden entitlement epoch omitted")
	}
	r.EntitlementEpoch--
	r.UsedBytes++
	if proofFactDigest(r) == before {
		t.Fatal("settled amount omitted")
	}
}

func TestNativeProofArchiveReferenceIsBoundedAndCanonical(t *testing.T) {
	ref := archiveReference(strings.Repeat("a", 64))
	if _, err := parseArchiveReference(ref); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{ref + " ", ref + ref, strings.Replace(ref, "\"version\":1", "\"version\":2", 1), strings.Repeat("x", 257)} {
		if _, err := parseArchiveReference(bad); err == nil {
			t.Fatal("ambiguous archive reference accepted", bad)
		}
	}
}
