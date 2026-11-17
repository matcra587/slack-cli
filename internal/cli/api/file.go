package api

import (
	"os"

	xfilepath "github.com/gechr/x/filepath"
)

// Exclusive creation rejects existing files and symlinks without following them.
// Keep the handle open so a later write cannot be redirected to another path.
type responseFile struct {
	file      *os.File
	identity  os.FileInfo
	committed bool
}

func createResponseFile(path string) (*responseFile, error) {
	file, err := os.OpenFile(xfilepath.Expand(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	identity, err := file.Stat()
	if err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, err
	}
	return &responseFile{file: file, identity: identity}, nil
}

func (f *responseFile) write(body []byte) error {
	if _, err := f.file.Write(body); err != nil {
		return err
	}
	if _, err := f.file.Write([]byte("\n")); err != nil {
		return err
	}
	if err := f.file.Close(); err != nil {
		return err
	}
	f.committed = true
	return nil
}

func (f *responseFile) cleanup() {
	if f.committed {
		return
	}
	pathInfo, pathErr := os.Lstat(f.file.Name())
	if pathErr == nil && os.SameFile(f.identity, pathInfo) {
		_ = os.Remove(f.file.Name())
	}
	_ = f.file.Close()
}
