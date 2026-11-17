package localfiles

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/experience"
)

type Store struct {
	Directory string
	mu        sync.Mutex
}

func (s *Store) ContentType(data []byte) string { return http.DetectContentType(data) }

func (s *Store) root() (*os.Root, error) {
	if s.Directory == "" {
		return nil, errors.New("file storage directory is empty")
	}
	if err := os.MkdirAll(s.Directory, 0700); err != nil {
		return nil, err
	}
	return os.OpenRoot(s.Directory)
}

func validID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}

func (s *Store) Put(ctx context.Context, id string, data []byte) error {
	if !validID(id) {
		return errors.New("invalid file identity")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.root()
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.OpenFile(id, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		file.Close()
		if !complete {
			_ = root.Remove(id)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func (s *Store) Open(ctx context.Context, id string) (experience.ReadSeekCloser, error) {
	if !validID(id) {
		return nil, experience.ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := s.root()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, experience.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, experience.ErrNotFound
	}
	return file, nil
}

func (s *Store) Remove(ctx context.Context, id string) error {
	if !validID(id) {
		return experience.ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := s.root()
	if err != nil {
		return err
	}
	defer root.Close()
	err = root.Remove(id)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
