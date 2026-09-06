package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

func sampleStorage(dir string) error {
	files := make(map[string]int64)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil // A rotated event segment may disappear between directory reads.
		}
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		name, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[name] = info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	free, err := diskFreeBytes(dir)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"at": time.Now().UTC(), "files": files, "disk_free_bytes": free})
}
