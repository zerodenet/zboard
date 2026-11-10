package zeroevent

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
)

type compressedMetadata struct {
	records       uint64
	originalBytes int64
	consumed      uint64
}

// Counts and cursor bounds are diagnostic metadata, not integrity receipts.
// Seek over compressed payloads; ReadBatch verifies/decompresses every record
// before projection. A status poll must never decode the entire backlog.
func inspectCompressedMetadata(path string, checkpoint *Checkpoint) (compressedMetadata, error) {
	var out compressedMetadata
	file, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return out, err
	}
	var offset int64
	var block uint64
	found := checkpoint == nil || checkpoint.Block == 0
	for offset < info.Size() {
		if info.Size()-offset < compressedBlockHeaderSize {
			return out, io.ErrUnexpectedEOF
		}
		header, err := readCompressedBlockHeader(file)
		if err != nil {
			return out, err
		}
		if expected := compressedCodecFromPath(path); expected != "" {
			actual, _ := compressionCodecName(header.Codec)
			if actual != expected {
				return out, errors.New("compressed segment codec mismatch")
			}
		}
		offset += compressedBlockHeaderSize
		if int64(header.CompressedLength) > info.Size()-offset {
			return out, io.ErrUnexpectedEOF
		}
		if uint64(header.RecordCount) > uint64(header.OriginalLength)/recordHeaderSize {
			return out, errors.New("compressed record count exceeds original byte bounds")
		}
		if math.MaxUint64-out.records < uint64(header.RecordCount) || out.originalBytes > math.MaxInt64-int64(header.OriginalLength) {
			return out, errors.New("compressed metadata count overflow")
		}
		block++
		if checkpoint != nil && checkpoint.Block == block {
			if checkpoint.Record > uint64(header.RecordCount) {
				return out, fmt.Errorf("checkpoint record %d exceeds compressed block %d record count %d", checkpoint.Record, block, header.RecordCount)
			}
			out.consumed = out.records + checkpoint.Record
			found = true
		}
		out.records += uint64(header.RecordCount)
		out.originalBytes += int64(header.OriginalLength)
		offset += int64(header.CompressedLength)
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return out, err
		}
	}
	if checkpoint != nil {
		if checkpoint.Block == 0 {
			if checkpoint.Record > out.records {
				return out, fmt.Errorf("legacy checkpoint record %d exceeds compressed segment record count %d", checkpoint.Record, out.records)
			}
			out.consumed = checkpoint.Record
		} else if !found {
			return out, fmt.Errorf("compressed checkpoint block %d does not exist", checkpoint.Block)
		}
	}
	return out, nil
}

// Validate the entire decoded block's framing before publishing a read horizon.
// Otherwise a damaged RecordCount could hide trailing events when a limited
// batch ends exactly on the falsely declared block boundary. This validates
// CRCs and bounds without allocating/decoding JSON for unconsumed records.
func validateCompressedRecordFrames(decoded []byte, count uint32) error {
	offset := 0
	for range count {
		if len(decoded)-offset < recordHeaderSize {
			return io.ErrUnexpectedEOF
		}
		h := decoded[offset : offset+recordHeaderSize]
		if binary.BigEndian.Uint32(h[:4]) != recordMagic {
			return errors.New("compressed record magic mismatch")
		}
		if binary.BigEndian.Uint16(h[4:6]) != recordVersion {
			return errors.New("compressed record version mismatch")
		}
		length := binary.BigEndian.Uint32(h[8:12])
		offset += recordHeaderSize
		if length > maxRecordPayloadSize || uint64(length) > uint64(len(decoded)-offset) {
			return errors.New("compressed record length exceeds block bounds")
		}
		payload := decoded[offset : offset+int(length)]
		if crc32.ChecksumIEEE(payload) != binary.BigEndian.Uint32(h[12:16]) {
			return errors.New("compressed record checksum mismatch")
		}
		offset += int(length)
	}
	if offset != len(decoded) {
		return errors.New("compressed block contains trailing record bytes")
	}
	return nil
}
