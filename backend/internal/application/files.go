package application

import "github.com/zerodenet/zboard/backend/internal/adapters/localfiles"

func (s *Services) ConfigureFileStorage(directory string) {
	if directory == "" {
		directory = "/var/lib/zboard/files"
	}
	store := &localfiles.Store{Directory: directory}
	s.Files.Blobs, s.Files.Inspector = store, store
}
