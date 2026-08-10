package zeroevent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrSpoolNotStarted = errors.New("zero event spool is not started")
	ErrSpoolClosed     = errors.New("zero event spool is closed")
)

type appendRequest struct {
	ctx   context.Context
	event Envelope
	done  chan error
}

type snapshotRequest struct {
	ctx  context.Context
	done chan snapshotResult
}

type snapshotResult struct {
	snapshot segmentSnapshot
	err      error
}

type closeRequest struct {
	done chan error
}

type activeSegment struct {
	sequence  uint64
	path      string
	file      *os.File
	createdAt time.Time
	size      int64
	records   uint64
}

type segmentSnapshot struct {
	sequence uint64
	path     string
	size     int64
	active   bool
}

// FileSpool is a single-process durable append log. Append calls are grouped
// into one write and fsync while ReadBatch consumes a stable snapshot of the
// active segment plus all sealed segments.
type FileSpool struct {
	cfg Config

	mu         sync.RWMutex
	started    bool
	closed     bool
	ctx        context.Context
	cancel     context.CancelFunc
	appendCh   chan appendRequest
	snapshotCh chan snapshotRequest
	closeCh    chan closeRequest
	writerDone chan struct{}
	active     *activeSegment // owned by the writer goroutine

	readMu     sync.Mutex
	checkpoint Checkpoint

	// segmentMu protects representation changes such as .active -> .ready,
	// .ready -> .ready.zst, and deletion after a committed checkpoint. The
	// actual compression work happens outside this lock so HTTP append traffic
	// never waits on zstd CPU work.
	segmentMu sync.RWMutex

	compressionMu   sync.Mutex
	compressionCh   chan struct{}
	compressionDone chan struct{}

	storageMu   sync.Mutex
	storageCh   chan struct{}
	storageDone chan struct{}
}

func NewFileSpool(cfg Config) (*FileSpool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, errors.New("cannot create file spool from disabled config")
	}
	if cfg.Driver != DriverFile {
		return nil, fmt.Errorf("file spool requires driver %q", DriverFile)
	}
	return &FileSpool{
		cfg:             cfg,
		appendCh:        make(chan appendRequest),
		snapshotCh:      make(chan snapshotRequest),
		closeCh:         make(chan closeRequest),
		writerDone:      make(chan struct{}),
		compressionCh:   make(chan struct{}, 1),
		compressionDone: make(chan struct{}),
		storageCh:       make(chan struct{}, 1),
		storageDone:     make(chan struct{}),
	}, nil
}

func (s *FileSpool) Start(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSpoolClosed
	}
	if s.started {
		return nil
	}
	if err := os.MkdirAll(s.cfg.Directory, 0o750); err != nil {
		return fmt.Errorf("create spool directory: %w", err)
	}
	// Reserve creation is best-effort. A host that is already too full to create
	// the reserve must still be allowed to recover and consume an existing spool.
	_ = s.ensureEmergencyReserve()
	checkpoint, err := loadCheckpoint(s.cfg.Directory)
	if err != nil {
		return err
	}
	active, err := recoverSegments(s.cfg.Directory)
	if err != nil {
		return err
	}
	s.checkpoint = checkpoint
	s.active = active
	s.ctx, s.cancel = context.WithCancel(parent)
	s.started = true
	go s.writerLoop()
	go s.storageLoop()
	s.requestStorageMaintenance()
	if s.compressionEnabled() {
		go s.compressionLoop()
		s.requestCompression()
	} else {
		close(s.compressionDone)
	}
	return nil
}

func (s *FileSpool) Append(ctx context.Context, event Envelope) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.RLock()
	started, closed := s.started, s.closed
	spoolCtx := s.ctx
	s.mu.RUnlock()
	if closed {
		return ErrSpoolClosed
	}
	if !started {
		return ErrSpoolNotStarted
	}
	request := appendRequest{ctx: ctx, event: event, done: make(chan error, 1)}
	select {
	case s.appendCh <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-spoolCtx.Done():
		return ErrSpoolClosed
	}
	select {
	case err := <-request.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-spoolCtx.Done():
		select {
		case err := <-request.done:
			return err
		default:
			return ErrSpoolClosed
		}
	}
}

func (s *FileSpool) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	if !s.started {
		s.closed = true
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	request := closeRequest{done: make(chan error, 1)}
	s.mu.Unlock()

	var err error
	select {
	case s.closeCh <- request:
		err = <-request.done
		<-s.writerDone
	case <-s.writerDone:
	}
	<-s.compressionDone
	<-s.storageDone
	return err
}

func (s *FileSpool) snapshot(ctx context.Context) (segmentSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.RLock()
	started, closed := s.started, s.closed
	spoolCtx := s.ctx
	s.mu.RUnlock()
	if !started {
		return segmentSnapshot{}, ErrSpoolNotStarted
	}
	if closed {
		return segmentSnapshot{}, ErrSpoolClosed
	}
	request := snapshotRequest{ctx: ctx, done: make(chan snapshotResult, 1)}
	select {
	case s.snapshotCh <- request:
	case <-ctx.Done():
		return segmentSnapshot{}, ctx.Err()
	case <-spoolCtx.Done():
		return segmentSnapshot{}, ErrSpoolClosed
	}
	select {
	case result := <-request.done:
		return result.snapshot, result.err
	case <-ctx.Done():
		return segmentSnapshot{}, ctx.Err()
	case <-spoolCtx.Done():
		return segmentSnapshot{}, ErrSpoolClosed
	}
}

func (s *FileSpool) writerLoop() {
	defer close(s.writerDone)
	ageTicker := time.NewTicker(minDuration(s.cfg.Segment.MaxAge/4, time.Second))
	defer ageTicker.Stop()

	for {
		select {
		case first := <-s.appendCh:
			s.handleAppendBatch(first)
		case request := <-s.snapshotCh:
			request.done <- snapshotResult{snapshot: s.currentSnapshot()}
		case request := <-s.closeCh:
			request.done <- s.closeActive()
			if s.cancel != nil {
				s.cancel()
			}
			return
		case <-ageTicker.C:
			if s.active != nil && s.active.records > 0 && time.Since(s.active.createdAt) >= s.cfg.Segment.MaxAge {
				_ = s.sealActive()
			}
		case <-s.ctx.Done():
			_ = s.closeActive()
			return
		}
	}
}

func (s *FileSpool) handleAppendBatch(first appendRequest) {
	batch := []appendRequest{first}
	timer := time.NewTimer(s.cfg.Flush.Interval)
	defer timer.Stop()
	var pendingSnapshot *snapshotRequest
	var pendingClose *closeRequest

collect:
	for len(batch) < s.cfg.Flush.MaxBatch {
		select {
		case next := <-s.appendCh:
			batch = append(batch, next)
		case request := <-s.snapshotCh:
			pendingSnapshot = &request
			break collect
		case request := <-s.closeCh:
			pendingClose = &request
			break collect
		case <-timer.C:
			break collect
		case <-s.ctx.Done():
			break collect
		}
	}

	err := s.writeBatch(batch)
	for _, request := range batch {
		request.done <- err
	}
	if pendingSnapshot != nil {
		pendingSnapshot.done <- snapshotResult{snapshot: s.currentSnapshot(), err: err}
	}
	if pendingClose != nil {
		closeErr := s.closeActive()
		if err == nil {
			err = closeErr
		}
		pendingClose.done <- err
		if s.cancel != nil {
			s.cancel()
		}
	}
}

func (s *FileSpool) writeBatch(batch []appendRequest) error {
	if len(batch) == 0 {
		return nil
	}
	records := make([][]byte, 0, len(batch))
	var totalBytes int64
	for _, request := range batch {
		if err := request.ctx.Err(); err != nil {
			return err
		}
		record, err := encodeRecord(request.event)
		if err != nil {
			return err
		}
		if int64(len(record)) > s.cfg.Segment.MaxSize {
			return fmt.Errorf("event record size %d exceeds segment max size %d", len(record), s.cfg.Segment.MaxSize)
		}
		records = append(records, record)
		totalBytes += int64(len(record))
	}

	// This call never rejects because a soft threshold was crossed. It only
	// performs reclamation before the writer attempts the actual durable append.
	s.prepareAppend(totalBytes)

	for _, record := range records {
		if s.active != nil && s.active.records > 0 && s.active.size+int64(len(record)) > s.cfg.Segment.MaxSize {
			if err := s.sealActive(); err != nil {
				return err
			}
		}
		if s.active == nil {
			active, err := createActiveSegment(s.cfg.Directory)
			if err != nil {
				return err
			}
			s.active = active
		}
		if err := s.writeRecordWithCapacityRecovery(record); err != nil {
			return err
		}
	}
	if s.active != nil {
		if err := s.syncActiveWithCapacityRecovery(); err != nil {
			return err
		}
	}
	return nil
}

func (s *FileSpool) currentSnapshot() segmentSnapshot {
	if s.active == nil {
		return segmentSnapshot{}
	}
	return segmentSnapshot{sequence: s.active.sequence, path: s.active.path, size: s.active.size, active: true}
}

func (s *FileSpool) sealActive() error {
	if s.active == nil {
		return nil
	}
	active := s.active
	if err := active.file.Sync(); err != nil {
		return fmt.Errorf("sync active segment before seal: %w", err)
	}

	s.segmentMu.Lock()
	defer s.segmentMu.Unlock()
	if err := active.file.Close(); err != nil {
		return fmt.Errorf("close active segment: %w", err)
	}
	readyPath := segmentPath(s.cfg.Directory, active.sequence, segmentReadySuffix)
	if err := os.Rename(active.path, readyPath); err != nil {
		return fmt.Errorf("seal active segment: %w", err)
	}
	if err := syncDirectory(s.cfg.Directory); err != nil {
		return err
	}
	s.active = nil
	go s.requestCompression()
	return nil
}

func (s *FileSpool) closeActive() error {
	if s.active == nil {
		return nil
	}
	if err := s.active.file.Sync(); err != nil {
		return err
	}
	return s.active.file.Close()
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 || a > b {
		return b
	}
	return a
}

func syncDirectory(directory string) error {
	file, err := os.Open(filepath.Clean(directory))
	if err != nil {
		return fmt.Errorf("open spool directory for sync: %w", err)
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync spool directory: %w", err)
	}
	return nil
}
