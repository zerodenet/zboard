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
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
)

const (
	compressedBlockMagic      uint32 = 0x5a454231 // ZEB1
	compressedBlockVersion    uint16 = 1
	compressedBlockCodecZstd  uint16 = 1
	compressedBlockCodecLZ4   uint16 = 2
	compressedBlockHeaderSize        = 24
	segmentZstdSuffix                = ".ready.zst"
	segmentLZ4Suffix                 = ".ready.lz4"
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
			if err := s.compressReadySegments(); err != nil {
				log.Printf("Zero event spool compression failed: codec=%s error=%v", strings.TrimSpace(s.cfg.Compression.Algorithm), err)
			}
		case <-ticker.C:
			if err := s.compressReadySegments(); err != nil {
				log.Printf("Zero event spool compression retry failed: codec=%s error=%v", strings.TrimSpace(s.cfg.Compression.Algorithm), err)
			}
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
	if !s.cfg.Compression.Enabled {
		return false
	}
	switch strings.TrimSpace(s.cfg.Compression.Algorithm) {
	case CompressionZstd, CompressionLZ4:
		return true
	default:
		return false
	}
}

func compressionCodec(algorithm string) (uint16, string, error) {
	switch strings.TrimSpace(algorithm) {
	case CompressionZstd:
		return compressedBlockCodecZstd, segmentZstdSuffix, nil
	case CompressionLZ4:
		return compressedBlockCodecLZ4, segmentLZ4Suffix, nil
	default:
		return 0, "", fmt.Errorf("unsupported compression algorithm %q", algorithm)
	}
}

func compressionCodecName(codec uint16) (string, error) {
	switch codec {
	case compressedBlockCodecZstd:
		return CompressionZstd, nil
	case compressedBlockCodecLZ4:
		return CompressionLZ4, nil
	default:
		return "", fmt.Errorf("unsupported compressed block codec %d", codec)
	}
}

func compressedCodecFromPath(path string) string {
	switch {
	case strings.HasSuffix(path, segmentZstdSuffix):
		return CompressionZstd
	case strings.HasSuffix(path, segmentLZ4Suffix):
		return CompressionLZ4
	default:
		return ""
	}
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
	algorithm := strings.TrimSpace(s.cfg.Compression.Algorithm)
	_, suffix, err := compressionCodec(algorithm)
	if err != nil {
		return err
	}
	finalPath := segmentPath(s.cfg.Directory, segment.sequence, suffix)
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
	if err := compressRawSegmentWithCodec(source, temporary, s.cfg.Compression.BlockSize, algorithm, s.cfg.Compression.Level, s.cfg.Compression.Workers); err != nil {
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
	if _, _, err := inspectCompressedSegmentForCodec(temporaryPath, algorithm); err != nil {
		return fmt.Errorf("validate compressed segment: %w", err)
	}
	if err := s.finalizeCompressedSegment(segment.path, finalPath, temporaryPath); err != nil {
		return err
	}
	if info, err := os.Stat(finalPath); err == nil {
		saved := segment.size - info.Size()
		log.Printf("Zero event spool segment compressed: sequence=%d codec=%s before_bytes=%d after_bytes=%d saved_bytes=%d", segment.sequence, algorithm, segment.size, info.Size(), saved)
	}
	return nil
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
	return compressRawSegmentWithCodec(source, target, blockSize, CompressionZstd, level, workers)
}

func compressRawSegmentWithCodec(source io.Reader, target io.Writer, blockSize int64, algorithm string, level, workers int) error {
	if blockSize <= 0 {
		return errors.New("compression block size must be greater than zero")
	}
	if workers <= 0 {
		return errors.New("compression workers must be greater than zero")
	}
	codec, _, err := compressionCodec(algorithm)
	if err != nil {
		return err
	}

	var zstdEncoder *zstd.Encoder
	if algorithm == CompressionZstd {
		zstdEncoder, err = zstd.NewWriter(nil,
			zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)),
			zstd.WithEncoderConcurrency(workers),
		)
		if err != nil {
			return fmt.Errorf("create zstd encoder: %w", err)
		}
		defer zstdEncoder.Close()
	}

	compress := func(block []byte) ([]byte, error) {
		switch algorithm {
		case CompressionZstd:
			return zstdEncoder.EncodeAll(block, nil), nil
		case CompressionLZ4:
			return encodeLZ4Frame(block, level, workers)
		default:
			return nil, fmt.Errorf("unsupported compression algorithm %q", algorithm)
		}
	}

	reader := bufio.NewReader(source)
	block := make([]byte, 0, blockSize)
	var records uint32
	flush := func() error {
		if records == 0 {
			return nil
		}
		compressed, err := compress(block)
		if err != nil {
			return err
		}
		if uint64(len(compressed)) > uint64(^uint32(0)) || uint64(len(block)) > uint64(^uint32(0)) {
			return errors.New("compressed block exceeds format limit")
		}
		header := compressedBlockHeader{
			Magic:            compressedBlockMagic,
			Version:          compressedBlockVersion,
			Codec:            codec,
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

func encodeLZ4Frame(block []byte, level, workers int) ([]byte, error) {
	compressionLevel, err := lz4CompressionLevel(level)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer := lz4.NewWriter(&buffer)
	if err := writer.Apply(
		lz4.BlockSizeOption(lz4.Block256Kb),
		lz4.CompressionLevelOption(compressionLevel),
		lz4.ConcurrencyOption(workers),
		lz4.ChecksumOption(true),
	); err != nil {
		return nil, fmt.Errorf("configure lz4 encoder: %w", err)
	}
	if _, err := writer.Write(block); err != nil {
		return nil, fmt.Errorf("encode lz4 block: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close lz4 encoder: %w", err)
	}
	return buffer.Bytes(), nil
}

func lz4CompressionLevel(level int) (lz4.CompressionLevel, error) {
	switch level {
	case 0:
		return lz4.Fast, nil
	case 1:
		return lz4.Level1, nil
	case 2:
		return lz4.Level2, nil
	case 3:
		return lz4.Level3, nil
	case 4:
		return lz4.Level4, nil
	case 5:
		return lz4.Level5, nil
	case 6:
		return lz4.Level6, nil
	case 7:
		return lz4.Level7, nil
	case 8:
		return lz4.Level8, nil
	case 9:
		return lz4.Level9, nil
	default:
		return 0, fmt.Errorf("lz4 compression level must be between 0 and 9, got %d", level)
	}
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
	if _, err := compressionCodecName(header.Codec); err != nil {
		return header, err
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
	var decoded []byte
	switch header.Codec {
	case compressedBlockCodecZstd:
		decoder, err := zstd.NewReader(nil)
		if err != nil {
			return nil, fmt.Errorf("create zstd decoder: %w", err)
		}
		decoded, err = decoder.DecodeAll(payload, nil)
		decoder.Close()
		if err != nil {
			return nil, fmt.Errorf("decode zstd block: %w", err)
		}
	case compressedBlockCodecLZ4:
		reader := lz4.NewReader(bytes.NewReader(payload))
		var err error
		decoded, err = io.ReadAll(io.LimitReader(reader, int64(header.OriginalLength)+1))
		if err != nil {
			return nil, fmt.Errorf("decode lz4 block: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported compressed block codec %d", header.Codec)
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
	return inspectCompressedSegmentForCodec(path, compressedCodecFromPath(path))
}

func inspectCompressedSegmentForCodec(path, expectedCodec string) (uint64, int64, error) {
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
		if expectedCodec != "" {
			actualCodec, _ := compressionCodecName(header.Codec)
			if actualCodec != expectedCodec {
				return records, consumed, fmt.Errorf("compressed segment codec mismatch: got %s want %s", actualCodec, expectedCodec)
			}
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

func compressedSegmentOriginalBytes(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var total int64
	for {
		header, err := readCompressedBlockHeader(reader)
		if errors.Is(err, io.EOF) {
			return total, nil
		}
		if err != nil {
			return 0, err
		}
		if total > int64(^uint64(0)>>1)-int64(header.OriginalLength) {
			return 0, errors.New("compressed original byte count overflow")
		}
		total += int64(header.OriginalLength)
		if _, err := io.CopyN(io.Discard, reader, int64(header.CompressedLength)); err != nil {
			return 0, err
		}
	}
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
		actualCodec, _ := compressionCodecName(header.Codec)
		if segment.codec != CompressionNone && actualCodec != segment.codec {
			return nil, checkpoint, fmt.Errorf("compressed segment %s codec mismatch: got %s want %s", filepath.Base(segment.path), actualCodec, segment.codec)
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
