package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// backupFilenameRe validates backup filenames: {name}_{YYYY-MM-DDTHH-MM-SS}.db
var backupFilenameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,64}_\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.db$`)

// BackupMeta describes a single backup file.
type BackupMeta struct {
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt string `json:"created_at"`
}

// StorageBackend abstracts backup file storage.
type StorageBackend interface {
	Put(filename string, src string) (int64, error)
	Get(filename string, dst string) error
	List(prefix string) ([]BackupMeta, error)
	Delete(filename string) error
	Exists(filename string) (bool, error)
}

// LocalStorage stores backups on the local filesystem.
type LocalStorage struct {
	dir string
}

func newLocalStorage(dir string) (*LocalStorage, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}
	return &LocalStorage{dir: dir}, nil
}

func (s *LocalStorage) Put(filename, src string) (int64, error) {
	dst := filepath.Join(s.dir, filename)
	n, err := copyFile(src, dst)
	return n, err
}

func (s *LocalStorage) Get(filename, dst string) error {
	src := filepath.Join(s.dir, filename)
	_, err := copyFile(src, dst)
	return err
}

func (s *LocalStorage) List(prefix string) ([]BackupMeta, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var result []BackupMeta
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if !backupFilenameRe.MatchString(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, BackupMeta{
			Filename:  name,
			SizeBytes: info.Size(),
			CreatedAt: createdAtFromFilename(name),
		})
	}
	// Newest first
	sort.Slice(result, func(i, j int) bool {
		return result[i].Filename > result[j].Filename
	})
	return result, nil
}

func (s *LocalStorage) Delete(filename string) error {
	path := filepath.Join(s.dir, filename)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *LocalStorage) Exists(filename string) (bool, error) {
	_, err := os.Stat(filepath.Join(s.dir, filename))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *LocalStorage) Path(filename string) string {
	return filepath.Join(s.dir, filename)
}

// backupFilename generates a timestamped backup filename.
func backupFilename(dbName string) string {
	ts := time.Now().UTC().Format("2006-01-02T15-04-05")
	return fmt.Sprintf("%s_%s.db", dbName, ts)
}

// createdAtFromFilename extracts the timestamp string from a backup filename.
func createdAtFromFilename(filename string) string {
	// filename: {name}_{YYYY-MM-DDTHH-MM-SS}.db
	base := strings.TrimSuffix(filename, ".db")
	// Find last underscore (separates name from timestamp)
	idx := strings.LastIndex(base, "_")
	if idx < 0 {
		return ""
	}
	return base[idx+1:]
}

// dbNameFromFilename extracts the database name from a backup filename.
func dbNameFromFilename(filename string) string {
	base := strings.TrimSuffix(filename, ".db")
	idx := strings.LastIndex(base, "_")
	if idx < 0 {
		return ""
	}
	return base[:idx]
}

// copyFile copies src to dst, returning bytes written.
func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	n, err := io.Copy(out, in)
	if err != nil {
		os.Remove(dst)
		return 0, err
	}
	return n, out.Sync()
}
