package firebase

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/storage"
)

// Storage provides upload helpers for Firebase Cloud Storage.
type Storage struct {
	app    *App
	bucket string
}

// NewStorage creates a Storage wrapper.
func (a *App) NewStorage() *Storage {
	if a == nil {
		return nil
	}
	return &Storage{app: a, bucket: a.StorageBucket}
}

// UploadScanImage uploads the original scan image and returns its public URL.
func (s *Storage) UploadScanImage(ctx context.Context, hash string, dataURL string) (string, error) {
	if s == nil || s.app == nil {
		return "", fmt.Errorf("storage not initialized")
	}

	contentType, ext, data, err := parseDataURL(dataURL)
	if err != nil {
		return "", err
	}

	client, err := s.app.StorageClient(ctx)
	if err != nil {
		return "", fmt.Errorf("storage client: %w", err)
	}

	bucket, err := client.Bucket(s.bucket)
	if err != nil {
		return "", fmt.Errorf("bucket %s: %w", s.bucket, err)
	}

	objectName := fmt.Sprintf("scans/%s.%s", hash, ext)
	obj := bucket.Object(objectName)
	writer := obj.NewWriter(ctx)
	writer.ContentType = contentType
	writer.Metadata = map[string]string{
		"cacheControl": "public, max-age=31536000, immutable",
	}
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return "", fmt.Errorf("write object: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close writer: %w", err)
	}

	publicURL := fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.bucket, objectName)
	return publicURL, nil
}

func parseDataURL(dataURL string) (contentType, ext string, data []byte, err error) {
	if strings.HasPrefix(dataURL, "data:") {
		parts := strings.SplitN(dataURL, ",", 2)
		if len(parts) != 2 {
			return "", "", nil, fmt.Errorf("invalid data URL")
		}
		meta := parts[0]
		data, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return "", "", nil, fmt.Errorf("decode base64: %w", err)
		}
		contentType = strings.TrimPrefix(strings.Split(meta, ";")[0], "data:")
		if strings.Contains(meta, "image/png") {
			ext = "png"
		} else if strings.Contains(meta, "image/webp") {
			ext = "webp"
		} else {
			ext = "jpg"
		}
		return contentType, ext, data, nil
	}

	data, err = base64.StdEncoding.DecodeString(dataURL)
	if err != nil {
		return "", "", nil, fmt.Errorf("decode base64: %w", err)
	}
	return "image/jpeg", "jpg", data, nil
}

// UploadToPath uploads a data-URL image to a specific path and returns its public URL.
func (s *Storage) UploadToPath(ctx context.Context, path, dataURL string, metadata map[string]string) (string, error) {
	if s == nil || s.app == nil {
		return "", fmt.Errorf("storage not initialized")
	}
	contentType, _, data, err := parseDataURL(dataURL)
	if err != nil {
		return "", err
	}
	client, err := s.app.StorageClient(ctx)
	if err != nil {
		return "", fmt.Errorf("storage client: %w", err)
	}
	bucket, err := client.Bucket(s.bucket)
	if err != nil {
		return "", fmt.Errorf("bucket %s: %w", s.bucket, err)
	}
	obj := bucket.Object(path)
	writer := obj.NewWriter(ctx)
	writer.ContentType = contentType
	writer.CacheControl = "public, max-age=31536000, immutable"
	writer.Metadata = metadata
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return "", fmt.Errorf("write object: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close writer: %w", err)
	}
	if err := obj.ACL().Set(ctx, storage.AllUsers, storage.RoleReader); err != nil {
		return "", fmt.Errorf("make public: %w", err)
	}
	return fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.bucket, path), nil
}

// Exists reports whether an object exists at path.
func (s *Storage) Exists(ctx context.Context, path string) (bool, error) {
	if s == nil || s.app == nil {
		return false, fmt.Errorf("storage not initialized")
	}
	client, err := s.app.StorageClient(ctx)
	if err != nil {
		return false, err
	}
	bucket, err := client.Bucket(s.bucket)
	if err != nil {
		return false, err
	}
	_, err = bucket.Object(path).Attrs(ctx)
	if err == nil {
		return true, nil
	}
	if err == storage.ErrObjectNotExist {
		return false, nil
	}
	return false, err
}

// NowUTC returns the current UTC time.
func NowUTC() time.Time { return time.Now().UTC() }
