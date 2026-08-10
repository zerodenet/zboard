package zeroevent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	compressedBlockMagic      uint32 = 0x5a454231 // ZEB1
	compressedBlockVersion    uint16 = 1
	compressedBlockCodecZstd  uint16 = 1
	compressedBlockHeaderSize        = 24
	segmentZstdSuffix                = ".ready.zst"
	compressionRetryInterval         = 30 * time.Second
)

type compressedBlockHeader struct {
	Magic            uint32
	Version          uint16
	Codec            uint16
	CompressedLength uint32
	OriginalLength   uint32
	RecordCount      uint32
	Checksum         uint32
}

func (s *FileSpool) compressionLoop() {
	defer close(s.compressionDone)
	ticker := time.NewTicker(compressionRetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.compressionCh:
			_ = s.compressReadySegments()
		case <-ticker.C:
			_ = s.compressReadySegments()
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *FileSpool) requestCompression() {
	if !s.compressionEnabled() {
		return
	}
	select {
	case s.compressionCh <- struct{}{}:
	default:
	}
}

func (s *FileSpool) compressionEnabled() bool {
	return s.cfg.Compression.Enabled && strings.TrimSpace(s.cfg.Compression.Algorithm) == CompressionZstd
}

func (s *FileSpool) compressReadySegments() error {
	if !s.compressionEnabled() {
		return nil
	}
	s.compressionMu.Lock()
	defer s.compressionMu.Unlock()

	s.segmentMu.RLock()
	segments, err := listSegments(s.cfg.Directory)
	s.segmentMu.RUnlock()
	if err != nil {
		return err
	}
	for _, segment := range segments {
		if segment.active || segment.codec != CompressionNone {
			continue
		}
		if err := s.compressReadySegment(segment); err != nil {
			return err
		}
	}
	return nil
}

func (s *FileSpool) compressReadySegment(segment segmentFile) error {
	if segment.active || segment.codec != CompressionNone {
		return nil
	}
	finalPath := segmentPath(s.cfg.Directory, segment.sequence, segmentZstdSuffix)
	if _, err := os.Stat(finalPath); err == nil {
		if _, _, inspectErr := inspectCompressedSegment(finalPath); inspectErr == nil {
			return s.finalizeCompressedSegment(segment.path, finalPath, "")
		}
		if _, quarantineErr := quarantineSegment(finalPath); quarantineErr != nil {
			return fmt.Errorf("quarantine invalid compressed segment: %w", quarantineErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	source, err := os.Open(segment.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open ready segment for compression: %w", err)
	}
	defer source.Close()

	temporary, err := os.CreateTemp(s.cfg.Directory, fmt.Sprintf(".%020d-compress-*.tmp", segment.sequence))
	if err != nil {
		return fmt.Errorf("create compressed segment temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return err
	}
	if err := compressRawSegmentToZstdWithWorkers(source, temporary, s.cfg.Compression.BlockSize, s.cfg.Compression.Level, s.cfg.Compression.Workers); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync compressed segment: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if _, _, err := inspectCompressedSegment(temporaryPath); err != nil {
		return fmt.Errorf("validate compressed segment: %w", err)
	}
	return s.finalizeCompressedSegment(segment.path, finalPath, temporaryPath)
}

func (s *FileSpool) finalizeCompressedSegment(rawPath, finalPath, temporaryPath string) error {
	s.segmentMu.Lock()
	defer s.segmentMu.Unlock()

	if _, err := os.Stat(rawPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if temporaryPath != "" {
		if _, err := os.Stat(finalPath); err == nil {
			if _, _, inspectErr := inspectCompressedSegment(finalPath); inspectErr != nil {
				return inspectErr
			}
		} else if errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(temporaryPath, finalPath); err != nil {
				return fmt.Errorf("publish compressed segment: %w", err)
			}
			if err := syncDirectory(s.cfg.Directory); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	if _, _, err := inspectCompressedSegment(finalPath); err != nil {
		return err
	}
	if err := os.Remove(rawPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove compressed source segment: %w", err)
	}
	return syncDirectory(s.cfg.Directory)
}

func compressRawSegmentToZstd(source io.Reader, target io.Writer, blockSize int64, level int) error {
	return compressRawSegmentToZstdWithWorkers(source, target, blockSize, level, 1)
}

func compressRawSegmentToZstdWithWorkers(source io.Reader, target io.Writer, blockSize int64, level, workers int) error {
	if blockSize <= 0 {
		return errors.New("compression block size must be greater than zero")
	}
	if workers <= 0 {
		return errors.New("compression workers must be greater than zero")
	}
	encoder, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)),
		zstd.WithEncoderConcurrency(workers),
	)
	if err != nil {
		return fmt.Errorf("create zstd encoder: %w", err)
	}
	defer encoder.Close()

	reader := bufio.NewReader(source)
	block := make([]byte, 0, blockSize)
	var records uint32
	flush := func() error {
		if records == 0 {
			return nil
		}
		compressed := encoder.EncodeAll(block, nil)
		if uint64(len(compressed)) > uint64(^uint32(0)) || uint64(len(block)) > uint64(^uint32(0)) {
			return errors.New("compressed block exceeds format limit")
		}
		header := compressedBlockHeader{
			Magic:            compressedBlockMagic,
			Version:          compressedBlockVersion,
			Codec:            compressedBlockCodecZstd,
			CompressedLength: uint32(len(compressed)),
			OriginalLength:   uint32(len(block)),
			RecordCount:      records,
			Checksum:         crc32.ChecksumIEEE(block),
		}
		if err := writeCompressedBlockHeader(target, header); err != nil {
			return err
		}
		if _, err := target.Write(compressed); err != nil {
			return fmt.Errorf("write compressed block: %w", err)
		}
		block = block[:0]
		records = 0
		return nil
	}

	for {
		event, _, err := readRecord(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read ready segment for compression: %w", err)
		}
		record, err := encodeRecord(event)
		if err != nil {
			return err
		}
		if len(block) > 0 && int64(len(block)+len(record)) > blockSize {
			if err := flush(); err != nil {
				return err
			}
		}
		block = append(block, record...)
		records++
	}
	return flush()
}

func writeCompressedBlockHeader(writer io.Writer, header compressedBlockHeader) error {
	buffer := make([]byte, compressedBlockHeaderSize)
	binary.BigEndian.PutUint32(buffer[0:4], header.Magic)
	binary.BigEndian.PutUint16(buffer[4:6], header.Version)
	binary.BigEndian.PutUint16(buffer[6:8], header.Codec)
	binary.BigEndian.PutUint32(buffer[8:12], header.CompressedLength)
	binary.BigEndian.PutUint32(buffer[12:16], header.OriginalLength)
	binary.BigEndian.PutUint32(buffer[16:20], header.RecordCount)
	binary.BigEndian.PutUint32(buffer[20:24], header.Checksum)
	if _, err := writer.Write(buffer); err != nil {
		return fmt.Errorf("write compressed block header: %w", err)
	}
	return nil
}

func readCompressedBlockHeader(reader io.Reader) (compressedBlockHeader, error) {
	var header compressedBlockHeader
	buffer := make([]byte, compressedBlockHeaderSize)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		return header, err
	}
	header = compressedBlockHeader{
		Magic:            binary.BigEndian.Uint32(buffer[0:4]),
		Version:          binary.BigEndian.Uint16(buffer[4:6]),
		Codec:            binary.BigEndian.Uint16(buffer[6:8]),
		CompressedLength: binary.BigEndian.Uint32(buffer[8:12]),
		OriginalLength:   binary.BigEndian.Uint32(buffer[12:16]),
		RecordCount:      binary.BigEndian.Uint32(buffer[16:20]),
		Checksum:         binary.BigEndian.Uint32(buffer[20:24]),
	}
	if header.Magic != compressedBlockMagic {
		return header, fmt.Errorf("invalid compressed block magic %x", header.Magic)
	}
	if header.Version != compressedBlockVersion {
		return header, fmt.Errorf("unsupported compressed block version %d", header.Version)
	}
	if header.Codec != compressedBlockCodecZstd {
		return header, fmt.Errorf("unsupported compressed block codec %d", header.Codec)
	}
	if header.CompressedLength == 0 || header.OriginalLength == 0 || header.RecordCount == 0 {
		return header, errors.New("compressed block has invalid empty metadata")
	}
	return header, nil
}

func decodeCompressedBlock(reader io.Reader, header compressedBlockHeader) ([]byte, error) {
	payload := make([]byte, int(header.CompressedLength))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	defer decoder.Close()
	decoded, err := decoder.DecodeAll(payload, nil)
	if err != nil {
		return nil, fmt.Errorf("decode zstd block: %w", err)
	}
	if len(decoded) != int(header.OriginalLength) {
		return nil, fmt.Errorf("compressed block length mismatch: got %d want %d", len(decoded), header.OriginalLength)
	}
	if checksum := crc32.ChecksumIEEE(decoded); checksum != header.Checksum {
		return nil, fmt.Errorf("compressed block checksum mismatch: got %08x want %08x", checksum, header.Checksum)
	}
	return decoded, nil
}

func inspectCompressedSegment(path string) (uint64, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, 0, err
	}
	reader := bufio.NewReader(file)
	var records uint64
	var consumed int64
	for consumed < info.Size() {
		header, err := readCompressedBlockHeader(reader)
		if err != nil {
			return records, consumed, err
		}
		consumed += compressedBlockHeaderSize
		decoded, err := decodeCompressedBlock(reader, header)
		if err != nil {
			return records, consumed, err
		}
		consumed += int64(header.CompressedLength)
		blockReader := bufio.NewReader(bytes.NewReader(decoded))
		var blockRecords uint32
		for blockRecords < header.RecordCount {
			if _, _, err := readRecord(blockReader); err != nil {
				return records, consumed, fmt.Errorf("validate compressed event record: %w", err)
			}
			blockRecords++
			records++
		}
		if _, err := blockReader.Peek(1); err == nil {
			return records, consumed, errors.New("compressed block contains trailing record bytes")
		} else if !errors.Is(err, io.EOF) {
			return records, consumed, fmt.Errorf("inspect compressed block tail: %w", err)
		}
	}
	if consumed != info.Size() {
		return records, consumed, errors.New("compressed segment has trailing bytes")
	}
	return records, consumed, nil
}

func readCompressedSegmentBatch(ctx context.Context, segment segmentFile, checkpoint Checkpoint, limit int) ([]Envelope, Checkpoint, error) {
	file, err := os.Open(segment.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, checkpoint, nil
		}
		return nil, checkpoint, fmt.Errorf("open compressed event segment: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	legacyRemaining := checkpoint.Record
	startBlock := checkpoint.Block
	startRecord := checkpoint.Record
	if startBlock == 0 {
		startRecord = 0
	}

	events := make([]Envelope, 0, limit)
	var blockIndex uint64
	for len(events) < limit {
		if err := ctx.Err(); err != nil {
			return nil, checkpoint, err
		}
		header, err := readCompressedBlockHeader(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, checkpoint, fmt.Errorf("read compressed block header from %s: %w", filepath.Base(segment.path), err)
		}
		blockIndex++

		blockStartRecord := uint64(0)
		if startBlock == 0 {
			if legacyRemaining >= uint64(header.RecordCount) {
				legacyRemaining -= uint64(header.RecordCount)
				if _, err := io.CopyN(io.Discard, reader, int64(header.CompressedLength)); err != nil {
					return nil, checkpoint, err
				}
				continue
			}
			blockStartRecord = legacyRemaining
			startBlock = blockIndex
		} else if blockIndex < startBlock {
			if _, err := io.CopyN(io.Discard, reader, int64(header.CompressedLength)); err != nil {
				return nil, checkpoint, err
			}
			continue
		} else if blockIndex == startBlock {
			blockStartRecord = startRecord
		}

		decoded, err := decodeCompressedBlock(reader, header)
		if err != nil {
			return nil, checkpoint, fmt.Errorf("decode compressed block from %s: %w", filepath.Base(segment.path), err)
		}
		blockReader := bufio.NewReader(bytes.NewReader(decoded))
		var recordIndex uint64
		for recordIndex < blockStartRecord {
			if _, _, err := readRecord(blockReader); err != nil {
				return nil, checkpoint, err
			}
			recordIndex++
		}
		for recordIndex < uint64(header.RecordCount) && len(events) < limit {
			event, _, err := readRecord(blockReader)
			if err != nil {
				return nil, checkpoint, err
			}
			events = append(events, event)
			recordIndex++
			checkpoint = Checkpoint{Segment: segment.sequence, Block: blockIndex, Record: recordIndex}
		}
		if len(events) >= limit {
			break
		}
		startRecord = 0
	}
	return events, checkpoint, nil
}
