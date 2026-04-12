package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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
	// WriteTo streams the backup file directly to w, used for HTTP downloads.
	WriteTo(filename string, w io.Writer) error
}

// LocalStorage stores backups on the local filesystem.
type LocalStorage struct {
	dir string
}

func newLocalStorage(dir string) (*LocalStorage, error) {
	return &LocalStorage{dir: dir}, nil
}

func (s *LocalStorage) Put(filename, src string) (int64, error) {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return 0, fmt.Errorf("create backup dir: %w", err)
	}
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

func (s *LocalStorage) WriteTo(filename string, w io.Writer) error {
	f, err := os.Open(filepath.Join(s.dir, filename))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// ── S3Storage ──────────────────────────────────────────────────────────────

// S3Storage stores backups in an Amazon S3 bucket (or any S3-compatible service).
type S3Storage struct {
	client *s3.Client
	bucket string
	prefix string // key prefix, e.g. "easydb-backups/"
}

// newS3Storage creates an S3Storage using standard AWS environment credentials
// (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION). Set
// cfg.BackupS3Endpoint for MinIO or Cloudflare R2 compatibility.
func newS3Storage(cfg *Config) (*S3Storage, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.BackupS3Region),
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	clientOpts := []func(*s3.Options){}
	if cfg.BackupS3Endpoint != "" {
		clientOpts = append(clientOpts,
			func(o *s3.Options) {
				o.BaseEndpoint = aws.String(cfg.BackupS3Endpoint)
				o.UsePathStyle = true // required by MinIO and R2
			},
		)
	}

	return &S3Storage{
		client: s3.NewFromConfig(awsCfg, clientOpts...),
		bucket: cfg.BackupS3Bucket,
		prefix: cfg.BackupS3Prefix,
	}, nil
}

func (s *S3Storage) key(filename string) string {
	return s.prefix + filename
}

func (s *S3Storage) Put(filename, src string) (int64, error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}

	_, err = s.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.key(filename)),
		Body:          f,
		ContentLength: aws.Int64(fi.Size()),
		ContentType:   aws.String("application/x-sqlite3"),
	})
	if err != nil {
		return 0, fmt.Errorf("s3 put %s: %w", filename, err)
	}
	return fi.Size(), nil
}

func (s *S3Storage) Get(filename, dst string) error {
	out, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(filename)),
	})
	if err != nil {
		return fmt.Errorf("s3 get %s: %w", filename, err)
	}
	defer out.Body.Close()

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, out.Body); err != nil {
		os.Remove(dst)
		return err
	}
	return f.Sync()
}

func (s *S3Storage) List(prefix string) ([]BackupMeta, error) {
	var result []BackupMeta
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.key(prefix)),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return nil, fmt.Errorf("s3 list: %w", err)
		}
		for _, obj := range page.Contents {
			filename := strings.TrimPrefix(aws.ToString(obj.Key), s.prefix)
			if !backupFilenameRe.MatchString(filename) {
				continue
			}
			var size int64
			if obj.Size != nil {
				size = *obj.Size
			}
			result = append(result, BackupMeta{
				Filename:  filename,
				SizeBytes: size,
				CreatedAt: createdAtFromFilename(filename),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Filename > result[j].Filename
	})
	return result, nil
}

func (s *S3Storage) Delete(filename string) error {
	_, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(filename)),
	})
	return err
}

func (s *S3Storage) Exists(filename string) (bool, error) {
	_, err := s.client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(filename)),
	})
	if err != nil {
		var nf *types.NotFound
		if errors.As(err, &nf) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *S3Storage) WriteTo(filename string, w io.Writer) error {
	out, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(filename)),
	})
	if err != nil {
		return fmt.Errorf("s3 get %s: %w", filename, err)
	}
	defer out.Body.Close()
	_, err = io.Copy(w, out.Body)
	return err
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
