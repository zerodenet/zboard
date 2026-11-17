package zeroevent

// Both checkpoint formats are converted using bounded block-header reads;
// payload integrity is checked by the consumer, not by metadata counting.
func compressedCheckpointRecordOffset(path string, checkpoint Checkpoint) (uint64, uint64, error) {
	metadata, err := inspectCompressedMetadata(path, &checkpoint)
	return metadata.consumed, metadata.records, err
}
