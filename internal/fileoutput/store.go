// Package fileoutput implements bounded application-message capture, independent
// of FIX sessions, sequence allocation and resend stores.
package fileoutput

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

type File interface {
	io.Writer
	Sync() error
	Close() error
}
type Entry struct {
	Name     string
	Size     int64
	Modified time.Time
}
type Store interface {
	List() ([]Entry, error)
	Create(time.Time) (File, error)
	Remove(string) error
	Close() error
}

var captureName = regexp.MustCompile(`^capture-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[0-9]+\.jsonl$`)

type directoryStore struct {
	root, lock string
	once       sync.Once
	closeErr   error
}

// OpenDirectory owns a dedicated output directory. The lease is intentionally
// conservative after a crash; operators must verify the old process is gone.
func OpenDirectory(directory string) (Store, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, errors.New("cannot create output directory")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("output directory must be a real directory")
	}
	root, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, errors.New("cannot resolve output directory")
	}
	s := &directoryStore{root: root, lock: filepath.Join(root, ".file-output.lock")}
	if err = os.Mkdir(s.lock, 0700); err != nil {
		return nil, errors.New("output directory already leased or unavailable")
	}
	return s, nil
}

func (s *directoryStore) List() ([]Entry, error) {
	dir, err := os.Open(s.root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(130)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 129 {
		return nil, errors.New("too many entries in output directory")
	}
	var out []Entry
	for _, entry := range entries {
		if entry.Name() == ".file-output.lock" {
			continue
		}
		if !captureName.MatchString(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			return nil, errors.New("output directory contains unrelated entries")
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("output entry is not a regular file")
		}
		out = append(out, Entry{Name: entry.Name(), Size: info.Size(), Modified: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Modified.Equal(out[j].Modified) {
			return out[i].Name < out[j].Name
		}
		return out[i].Modified.Before(out[j].Modified)
	})
	return out, nil
}
func (s *directoryStore) Create(now time.Time) (File, error) {
	return os.CreateTemp(s.root, "capture-"+now.UTC().Format("20060102T150405.000000000Z")+"-*.jsonl")
}
func (s *directoryStore) Remove(name string) error {
	if !captureName.MatchString(name) {
		return errors.New("invalid capture filename")
	}
	info, err := os.Lstat(filepath.Join(s.root, name))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("capture file is not regular")
	}
	return os.Remove(filepath.Join(s.root, name))
}
func (s *directoryStore) Close() error {
	s.once.Do(func() { s.closeErr = os.Remove(s.lock) })
	return s.closeErr
}
