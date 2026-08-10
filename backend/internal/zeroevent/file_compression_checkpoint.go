package zeroevent

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
)

// compressedCheckpointRecordOffset converts either checkpoint form to a
// global consumed-record offset. Block 0 is the legacy raw-segment form where
// Record is already global. Block >= 1 uses a 1-based block index and a record
// offset within that block.
func compressedCheckpointRecordOffset(path string, checkpoint Checkpoint) (uint64, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)

	var total uint64
	var consumed uint64
	var before uint64
	var blockIndex uint64
	foundBlock := checkpoint.Block == 0
	for {
		header, err := readCompressedBlockHeader(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, total, err
		}
		blockIndex++
		blockRecords := uint64(header.RecordCount)
		if checkpoint.Block != 0 && blockIndex == checkpoint.Block {
			if checkpoint.Record > blockRecords {
				return 0, total, fmt.Errorf("checkpoint record %d exceeds compressed block %d record count %d", checkpoint.Record, checkpoint.Block, blockRecords)
			}
			consumed = before + checkpoint.Record
			foundBlock = true
		}
		total += blockRecords
		before += blockRecords
		if _, err := io.CopyN(io.Discard, reader, int64(header.CompressedLength)); err != nil {
			return 0, total, err
		}
	}
	if checkpoint.Block == 0 {
		consumed = checkpoint.Record
		if consumed > total {
			return 0, total, fmt.Errorf("legacy checkpoint record %d exceeds compressed segment record count %d", checkpoint.Record, total)
		}
		return consumed, total, nil
	}
	if !foundBlock {
		return 0, total, fmt.Errorf("compressed checkpoint block %d does not exist", checkpoint.Block)
	}
	return consumed, total, nil
}
