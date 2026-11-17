package observationstore

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestObservationPrefixMatchesActualVersionOneEncoderAndAssetNormalization(t *testing.T) {
	for _, label := range []string{strings.Repeat("a", 64), strings.Repeat("B", 64), strings.Repeat("z", 64), "", strings.Repeat("a", 65)} {
		source := network.ObservationSource{Observation: network.NodeObservation{NodeID: 8, Latest: network.EventPosition{AssetTag: label}}}
		var encoded bytes.Buffer
		if err := network.WriteObservationSource(&encoded, source); err != nil {
			t.Fatal(err)
		}
		asset := ""
		if label == strings.Repeat("a", 64) || label == strings.Repeat("B", 64) {
			asset = label
		}
		h := model.NodeObservationIntent{NodeID: 8, AssetTag: asset, MaterialBytes: uint64(encoded.Len())}
		if err := verifyObservationPrefix(h, encoded.Bytes()); err != nil {
			t.Fatal("real version-one source rejected", label, err)
		}
		h.NodeID++
		if err := verifyObservationPrefix(h, encoded.Bytes()); !errors.Is(err, ErrMaterial) {
			t.Fatal("changed header node accepted", err)
		}
		h.NodeID--
		if err := verifyObservationPrefix(h, encoded.Bytes()[:23]); !errors.Is(err, ErrMaterial) {
			t.Fatal("truncated prefix accepted", err)
		}
		bad := append([]byte(nil), encoded.Bytes()...)
		binary.BigEndian.PutUint64(bad[16:24], ^uint64(0))
		if err := verifyObservationPrefix(h, bad); !errors.Is(err, ErrMaterial) {
			t.Fatal("impossible asset length accepted", err)
		}
	}
}

func TestObservationRetirementLegacyLargeHeaderMetadataTamperKeepsBody(t *testing.T) {
	for _, column := range []string{"node_id", "asset_tag"} {
		t.Run(column, func(t *testing.T) {
			a, _, node := ownershipFixture(t)
			source := retirementSource(node, "legacy-large-metadata", false)
			source.Payload = bytes.Repeat([]byte("L"), 3<<20)
			id := seedOldLargeSource(t, a, source)
			var before model.NodeObservationIntent
			if err := a.First(&before, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			budget := retirementBudget(t, a)
			var changed any = node.ID + 1
			if column == "asset_tag" {
				changed = strings.Repeat("b", 64)
			}
			if err := a.Model(&model.NodeObservationIntent{}).Where("id = ?", id).UpdateColumn(column, changed).Error; err != nil {
				t.Fatal(err)
			}
			now := before.CreatedAt.Add(MaterialMinimumAge + time.Hour)
			if _, err := (MaterialRetention{DB: a, Now: func() time.Time { return now }}).Retire(t.Context(), 1, MaxDrainBytes); !errors.Is(err, ErrMaterial) {
				t.Fatal("changed legacy identity accepted", err)
			}
			var after model.NodeObservationIntent
			if err := a.First(&after, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			var checkpoint struct{ State string }
			if err := a.Model(&model.NodeObservationRetirement{}).Select("state").Where("intent_id = ?", id).Take(&checkpoint).Error; err != nil {
				t.Fatal(err)
			}
			if after.MaterialRetiredAt != nil || after.Status == "retired" || checkpoint.State != "quarantined" {
				t.Fatal("tampered source retired", after, checkpoint)
			}
			var count int64
			if err := a.Model(&model.NodeObservationPart{}).Where("intent_id = ?", id).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if uint64(count) != before.PartCount || retirementBudget(t, a) != budget {
				t.Fatal("tampered source body/budget changed", count, before.PartCount, retirementBudget(t, a), budget)
			}
		})
	}
}
