package zeroevent

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	recordMagic          uint32 = 0x5a455631 // ZEV1
	recordVersion        uint16 = 1
	recordHeaderSize            = 16
	maxRecordPayloadSize        = 64 << 20
	segmentActiveSuffix         = ".active"
	segmentReadySuffix          = ".ready"
	checkpointFileName          = "checkpoint.json"
)

type recordHeader struct {
	Magic    uint32
	Version  uint16
	Flags    uint16
	Length   uint32
	Checksum uint32
}

type segmentFile struct {
	sequence uint64
	path     string
	active   bool
	codec    string
	size     int64
}

func encodeRecord(event Envelope) ([]byte, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("encode zero event: %w", err)
	}
	if uint64(len(payload)) > uint64(^uint32(0)) {
		return nil, errors.New("zero event payload exceeds record format limit")
	}
	record := make([]byte, recordHeaderSize+len(payload))
	binary.BigEndian.PutUint32(record[0:4], recordMagic)
	binary.BigEndian.PutUint16(record[4:6], recordVersion)
	binary.BigEndian.PutUint16(record[6:8], 0)
	binary.BigEndian.PutUint32(record[8:12], uint32(len(payload)))
	binary.BigEndian.PutUint32(record[12:16], crc32.ChecksumIEEE(payload))
	copy(record[recordHeaderSize:], payload)
	return record, nil
}

func readRecord(reader io.Reader) (Envelope, int64, error) {
	var event Envelope
	headerBytes := make([]byte, recordHeaderSize)
	if _, err := io.ReadFull(reader, headerBytes); err != nil {
		return event, 0, err
	}
	header := recordHeader{
		Magic:    binary.BigEndian.Uint32(headerBytes[0:4]),
		Version:  binary.BigEndian.Uint16(headerBytes[4:6]),
		Flags:    binary.BigEndian.Uint16(headerBytes[6:8]),
		Length:   binary.BigEndian.Uint32(headerBytes[8:12]),
		Checksum: binary.BigEndian.Uint32(headerBytes[12:16]),
	}
	if header.Magic != recordMagic {
		return event, 0, fmt.Errorf("invalid event record magic %x", header.Magic)
	}
	if header.Version != recordVersion {
		return event, 0, fmt.Errorf("unsupported event record version %d", header.Version)
	}
	if header.Length > maxRecordPayloadSize {
		return event, 0, fmt.Errorf("event record payload length %d exceeds limit %d", header.Length, maxRecordPayloadSize)
	}
	payload := make([]byte, int(header.Length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return event, 0, err
	}
	if actual := crc32.ChecksumIEEE(payload); actual != header.Checksum {
		return event, 0, fmt.Errorf("event record checksum mismatch: got %08x want %08x", actual, header.Checksum)
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return event, 0, fmt.Errorf("decode zero event: %w", err)
	}
	return event, int64(recordHeaderSize) + int64(header.Length), nil
}

func segmentPath(directory string, sequence uint64, suffix string) string {
	return filepath.Join(directory, fmt.Sprintf("%020d%s", sequence, suffix))
}

func parseSegmentName(name string) (uint64, bool, string, bool) {
	active := false
	codec := CompressionNone
	base := ""
	switch {
	case strings.HasSuffix(name, segmentActiveSuffix):
		active = true
		base = strings.TrimSuffix(name, segmentActiveSuffix)
	case strings.HasSuffix(name, segmentZstdSuffix):
		codec = CompressionZstd
		base = strings.TrimSuffix(name, segmentZstdSuffix)
	case strings.HasSuffix(name, segmentReadySuffix):
		base = strings.TrimSuffix(name, segmentReadySuffix)
	default:
		return 0, false, "", false
	}
	sequence, err := strconv.ParseUint(base, 10, 64)
	if err != nil {
		return 0, false, "", false
	}
	return sequence, active, codec, true
}

func segmentPreference(segment segmentFile) int {
	if segment.active {
		return 3
	}
	if segment.codec == CompressionNone {
		return 2
	}
	return 1
}

func listSegments(directory string) ([]segmentFile, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("list spool segments: %w", err)
	}
	bySequence := make(map[uint64]segmentFile, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		sequence, active, codec, ok := parseSegmentName(entry.Name())
		if !ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat spool segment %s: %w", entry.Name(), err)
		}
		candidate := segmentFile{
			sequence: sequence,
			path:     filepath.Join(directory, entry.Name()),
			active:   active,
			codec:    codec,
			size:     info.Size(),
		}
		if current, exists := bySequence[sequence]; !exists || segmentPreference(candidate) > segmentPreference(current) {
			bySequence[sequence] = candidate
		}
	}
	segments := make([]segmentFile, 0, len(bySequence))
	for _, segment := range bySequence {
		segments = append(segments, segment)
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].sequence < segments[j].sequence })
	return segments, nil
}

func createActiveSegment(directory string) (*activeSegment, error) {
	segments, err := listSegments(directory)
	if err != nil {
		return nil, err
	}
	var sequence uint64 = 1
	if len(segments) > 0 {
		sequence = segments[len(segments)-1].sequence + 1
	}
	path := segmentPath(directory, sequence, segmentActiveSuffix)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o640)
	if err != nil {
		return nil, fmt.Errorf("create active event segment: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		file.Close()
		return nil, err
	}
	return &activeSegment{sequence: sequence, path: path, file: file, createdAt: time.Now()}, nil
}

func recoverSegments(directory string) (*activeSegment, error) {
	segments, err := listSegments(directory)
	if err != nil {
		return nil, err
	}
	activeIndexes := make([]int, 0, 1)
	for i, segment := range segments {
		records, validSize, err := inspectSegment(segment.path, segment.size, segment.active)
		if err != nil {
			quarantined, quarantineErr := quarantineSegment(segment.path)
			if quarantineErr != nil {
				return nil, fmt.Errorf("recover segment %s: %v; quarantine: %w", filepath.Base(segment.path), err, quarantineErr)
			}
			return nil, fmt.Errorf("recover segment %s: %w; quarantined as %s", filepath.Base(segment.path), err, filepath.Base(quarantined))
		}
		segments[i].size = validSize
		if segment.active {
			activeIndexes = append(activeIndexes, i)
			if validSize != segment.size {
				if err := os.Truncate(segment.path, validSize); err != nil {
					return nil, fmt.Errorf("truncate incomplete active segment: %w", err)
				}
			}
		}
		_ = records
	}
	if len(activeIndexes) == 0 {
		return nil, nil
	}
	for _, index := range activeIndexes[:len(activeIndexes)-1] {
		segment := segments[index]
		readyPath := segmentPath(directory, segment.sequence, segmentReadySuffix)
		if err := os.Rename(segment.path, readyPath); err != nil {
			return nil, fmt.Errorf("seal recovered active segment: %w", err)
		}
	}
	if len(activeIndexes) > 1 {
		if err := syncDirectory(directory); err != nil {
			return nil, err
		}
	}
	segment := segments[activeIndexes[len(activeIndexes)-1]]
	file, err := os.OpenFile(segment.path, os.O_RDWR|os.O_APPEND, 0o640)
	if err != nil {
		return nil, fmt.Errorf("open recovered active segment: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	records, _, err := inspectSegment(segment.path, info.Size(), false)
	if err != nil {
		file.Close()
		return nil, err
	}
	return &activeSegment{
		sequence:  segment.sequence,
		path:      segment.path,
		file:      file,
		createdAt: info.ModTime(),
		size:      info.Size(),
		records:   records,
	}, nil
}

// inspectSegment validates records up to limit. When tolerateTrailing is true,
// a partial final header or payload is treated as an interrupted append and the
// last valid byte offset is returned for truncation.
func inspectSegment(path string, limit int64, tolerateTrailing bool) (uint64, int64, error) {
	if strings.HasSuffix(path, segmentZstdSuffix) {
		if tolerateTrailing {
			return 0, 0, errors.New("compressed segments cannot tolerate trailing data")
		}
		return inspectCompressedSegment(path)
	}
	return inspectRawSegment(path, limit, tolerateTrailing)
}

func inspectRawSegment(path string, limit int64, tolerateTrailing bool) (uint64, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	reader := bufio.NewReader(io.LimitReader(file, limit))
	var records uint64
	var offset int64
	for offset < limit {
		_, size, err := readRecord(reader)
		if err != nil {
			if tolerateTrailing && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
				return records, offset, nil
			}
			return records, offset, err
		}
		offset += size
		records++
	}
	return records, offset, nil
}

func quarantineSegment(path string) (string, error) {
	directory := filepath.Dir(path)
	target := fmt.Sprintf("%s.corrupt.%d", path, time.Now().UnixNano())
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	if err := syncDirectory(directory); err != nil {
		return target, err
	}
	return target, nil
}
