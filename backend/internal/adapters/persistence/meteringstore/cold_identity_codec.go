package meteringstore

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/model"
	"io"
	"sort"
)

const coldBlockBytes = 256 * 1024
const coldBlockEntries = 512
const coldReport uint8 = 1
const coldNonce uint8 = 2
const coldFloor uint8 = 3
const coldTarget uint8 = 4

var errColdIdentity = errors.New("计费补报身份段校验失败；已保留原有计费结果，请修复身份段后重试")

type coldIdentity struct {
	Weight  []byte
	Key     string
	Value   string
	Numbers []uint64
}

func coldRoute(weight []byte) uint32 {
	h := sha256.Sum256(weight)
	return binary.BigEndian.Uint32(h[:4])
}
func coldPrefix(route uint32, depth uint8) uint32 {
	if depth == 0 {
		return 0
	}
	return route >> (32 - depth)
}
func coldRaw(entries []coldIdentity) ([]byte, error) {
	sort.Slice(entries, func(i, j int) bool { return bytes.Compare(entries[i].Weight, entries[j].Weight) < 0 })
	var b bytes.Buffer
	b.WriteByte(1)
	put := func(v uint64) { var x [10]byte; n := binary.PutUvarint(x[:], v); b.Write(x[:n]) }
	put(uint64(len(entries)))
	var prev []byte
	for _, e := range entries {
		if len(e.Weight) == 0 || len(e.Weight) > 8192 || len(e.Key) > 1024 || len(e.Value) > 1024 || len(e.Numbers) > 8 || (prev != nil && bytes.Compare(prev, e.Weight) >= 0) {
			return nil, errColdIdentity
		}
		prefix := 0
		for prefix < len(prev) && prefix < len(e.Weight) && prev[prefix] == e.Weight[prefix] {
			prefix++
		}
		put(uint64(prefix))
		put(uint64(len(e.Weight) - prefix))
		b.Write(e.Weight[prefix:])
		put(uint64(len(e.Key)))
		b.WriteString(e.Key)
		put(uint64(len(e.Value)))
		b.WriteString(e.Value)
		put(uint64(len(e.Numbers)))
		for _, n := range e.Numbers {
			put(n)
		}
		prev = e.Weight
	}
	return b.Bytes(), nil
}
func encodeCold(entries []coldIdentity) (model.MeteringColdBlock, error) {
	raw, err := coldRaw(entries)
	if err != nil {
		return model.MeteringColdBlock{}, err
	}
	if len(entries) > coldBlockEntries || len(raw) > coldBlockBytes {
		return model.MeteringColdBlock{}, errColdIdentity
	}
	var b bytes.Buffer
	w, _ := gzip.NewWriterLevel(&b, gzip.BestSpeed)
	if _, err = w.Write(raw); err != nil {
		return model.MeteringColdBlock{}, err
	}
	if err = w.Close(); err != nil {
		return model.MeteringColdBlock{}, err
	}
	h := sha256.Sum256(raw)
	block := model.MeteringColdBlock{Version: 1, EntryCount: uint32(len(entries)), RawBytes: uint32(len(raw)), SHA256: hex.EncodeToString(h[:]), Payload: b.Bytes(), Bloom: make([]byte, 1024)}
	for _, e := range entries {
		coldBloomAdd(block.Bloom, e.Weight)
	}
	block.HeaderSHA256 = coldHeaderDigest(block)
	return block, nil
}
func decodeCold(block model.MeteringColdBlock) ([]coldIdentity, error) {
	if !validColdHeader(block) || block.Version != 1 || block.EntryCount > coldBlockEntries || block.RawBytes == 0 || block.RawBytes > coldBlockBytes || len(block.Payload) == 0 || len(block.Payload) > coldBlockBytes+1024 || block.Depth > 32 {
		return nil, errColdIdentity
	}
	input := bytes.NewReader(block.Payload)
	r, e := gzip.NewReader(input)
	if e != nil {
		return nil, errColdIdentity
	}
	r.Multistream(false)
	raw, e := io.ReadAll(io.LimitReader(r, int64(block.RawBytes)+1))
	closed := r.Close()
	h := sha256.Sum256(raw)
	if e != nil || closed != nil || input.Len() != 0 || len(raw) != int(block.RawBytes) || hex.EncodeToString(h[:]) != block.SHA256 {
		return nil, errColdIdentity
	}
	b := bytes.NewReader(raw)
	v, e := b.ReadByte()
	if e != nil || v != 1 {
		return nil, errColdIdentity
	}
	count, e := binary.ReadUvarint(b)
	if e != nil || count != uint64(block.EntryCount) {
		return nil, errColdIdentity
	}
	entries := make([]coldIdentity, 0, count)
	var prev []byte
	getBytes := func(max uint64) ([]byte, error) {
		n, e := binary.ReadUvarint(b)
		if e != nil || n > max || n > uint64(b.Len()) {
			return nil, errColdIdentity
		}
		x := make([]byte, n)
		_, e = io.ReadFull(b, x)
		return x, e
	}
	for i := uint64(0); i < count; i++ {
		prefix, e := binary.ReadUvarint(b)
		if e != nil || prefix > uint64(len(prev)) {
			return nil, errColdIdentity
		}
		suffix, e := getBytes(8192 - prefix)
		if e != nil {
			return nil, errColdIdentity
		}
		weight := append(append([]byte(nil), prev[:prefix]...), suffix...)
		key, e := getBytes(1024)
		if e != nil || len(weight) == 0 || coldPrefix(coldRoute(weight), block.Depth) != block.Prefix || (prev != nil && bytes.Compare(prev, weight) >= 0) {
			return nil, errColdIdentity
		}
		value, e := getBytes(1024)
		if e != nil {
			return nil, errColdIdentity
		}
		n, e := binary.ReadUvarint(b)
		if e != nil || n > 8 {
			return nil, errColdIdentity
		}
		var nums []uint64
		if n > 0 {
			nums = make([]uint64, n)
		}
		for j := range nums {
			nums[j], e = binary.ReadUvarint(b)
			if e != nil {
				return nil, errColdIdentity
			}
		}
		entries = append(entries, coldIdentity{Weight: weight, Key: string(key), Value: string(value), Numbers: nums})
		prev = weight
	}
	bloom := make([]byte, 1024)
	for _, e := range entries {
		coldBloomAdd(bloom, e.Weight)
	}
	if !bytes.Equal(bloom, block.Bloom) {
		return nil, errColdIdentity
	}
	if b.Len() != 0 {
		return nil, errColdIdentity
	}
	return entries, nil
}

// A negative filter is authoritative only with its intact header checksum.
// Positive matches always decode the complete identity; no probabilistic hit
// ever decides that a report has already been billed.
func coldBloomAdd(filter, weight []byte) {
	h := sha256.Sum256(weight)
	for i := 0; i < 6; i++ {
		n := binary.BigEndian.Uint32(h[4+i*4:8+i*4]) % 8192
		filter[n/8] |= byte(1 << (n % 8))
	}
}
func coldBloomMayContain(filter, weight []byte) bool {
	if len(filter) != 1024 {
		return true
	}
	h := sha256.Sum256(weight)
	for i := 0; i < 6; i++ {
		n := binary.BigEndian.Uint32(h[4+i*4:8+i*4]) % 8192
		if filter[n/8]&byte(1<<(n%8)) == 0 {
			return false
		}
	}
	return true
}
func coldHeaderDigest(b model.MeteringColdBlock) string {
	raw, _ := json.Marshal([]any{b.NodeID, b.Kind, b.Depth, b.Prefix, b.Version, b.EntryCount, b.RawBytes, b.SHA256, b.Bloom})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func validColdHeader(b model.MeteringColdBlock) bool {
	return len(b.Bloom) == 1024 && b.HeaderSHA256 == coldHeaderDigest(b)
}
