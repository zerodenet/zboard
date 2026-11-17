package experience

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const MaxUploadBytes int64 = 5 << 20
const MaxSiteImageBytes int64 = 2 << 20
const MaxUserFileBytes int64 = 100 << 20
const MaxStoredFileBytes int64 = 1 << 30

var ErrFileInUse = errors.New("file is already referenced")

type StoredFile struct {
	ID          string    `json:"id"`
	OwnerID     uint      `json:"-"`
	Purpose     string    `json:"purpose"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
}

func (f StoredFile) URL() string {
	if f.Purpose == "site" {
		return "/media/" + f.ID
	}
	return "/api/v1/files/" + f.ID
}

type FileRepository interface {
	SaveFile(context.Context, TicketActor, StoredFile) error
	ReadFile(context.Context, TicketActor, string, bool) (StoredFile, error)
	DeleteFile(context.Context, TicketActor, string, time.Time) (StoredFile, error)
}

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}
type FileBlobs interface {
	Put(context.Context, string, []byte) error
	Open(context.Context, string) (ReadSeekCloser, error)
	Remove(context.Context, string) error
}

type FileContentInspector interface{ ContentType([]byte) string }

type Files struct {
	Repository FileRepository
	Blobs      FileBlobs
	Inspector  FileContentInspector
}

func (s Files) Upload(ctx context.Context, actor TicketActor, purpose, name string, source io.Reader, now time.Time) (StoredFile, error) {
	if actor.ID == 0 || (purpose == "site" && !actor.IsAdmin) {
		return StoredFile{}, ErrPermission
	}
	if purpose != "site" && purpose != "ticket" {
		return StoredFile{}, fmt.Errorf("%w: invalid file purpose", ErrInvalid)
	}
	limit := MaxUploadBytes
	if purpose == "site" {
		limit = MaxSiteImageBytes
	}
	data, err := io.ReadAll(io.LimitReader(source, limit+1))
	if err != nil {
		return StoredFile{}, err
	}
	if len(data) == 0 || int64(len(data)) > limit {
		return StoredFile{}, fmt.Errorf("%w: file must contain 1 to %d bytes", ErrInvalid, limit)
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || len(name) > 255 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return StoredFile{}, fmt.Errorf("%w: invalid file name", ErrInvalid)
	}
	if s.Inspector == nil {
		return StoredFile{}, errors.New("file content inspector is not configured")
	}
	contentType, err := UploadContentType(purpose, name, data, s.Inspector.ContentType(data))
	if err != nil {
		return StoredFile{}, err
	}
	if s.Blobs == nil {
		return StoredFile{}, errors.New("file storage is not configured")
	}
	file := StoredFile{ID: uuid.NewString(), OwnerID: actor.ID, Purpose: purpose, Name: name, ContentType: contentType, Size: int64(len(data)), CreatedAt: now.UTC()}
	if err := s.Blobs.Put(ctx, file.ID, data); err != nil {
		return StoredFile{}, err
	}
	if err := s.Repository.SaveFile(ctx, actor, file); err != nil {
		_ = s.Blobs.Remove(context.WithoutCancel(ctx), file.ID)
		return StoredFile{}, err
	}
	return file, nil
}

func (s Files) Read(ctx context.Context, actor TicketActor, id string, publicOnly bool) (StoredFile, ReadSeekCloser, error) {
	if _, err := uuid.Parse(id); err != nil {
		return StoredFile{}, nil, ErrNotFound
	}
	file, err := s.Repository.ReadFile(ctx, actor, id, publicOnly)
	if err != nil {
		return StoredFile{}, nil, err
	}
	if s.Blobs == nil {
		return StoredFile{}, nil, errors.New("file storage is not configured")
	}
	blob, err := s.Blobs.Open(ctx, id)
	return file, blob, err
}

func (s Files) Delete(ctx context.Context, actor TicketActor, id string, now time.Time) error {
	if actor.ID == 0 {
		return ErrPermission
	}
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	file, err := s.Repository.DeleteFile(ctx, actor, id, now.UTC())
	if err != nil {
		return err
	}
	if s.Blobs == nil {
		return errors.New("file storage is not configured")
	}
	return s.Blobs.Remove(ctx, file.ID)
}
