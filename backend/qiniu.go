package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/qiniu/go-sdk/v7/auth"
	"github.com/qiniu/go-sdk/v7/storage"
)

const maxThumbnailBytes = 5 << 20

type thumbnailMirror interface {
	Mirror(context.Context, string) (string, error)
}

type qiniuUploadClient interface {
	Put(context.Context, interface{}, string, string, io.Reader, int64, *storage.PutExtra) error
}

type qiniuThumbnailMirror struct {
	credentials *auth.Credentials
	bucket      string
	domain      string
	uploader    qiniuUploadClient
	fetch       func(context.Context, string) ([]byte, string, error)
}

func newThumbnailMirrorFromEnv() (thumbnailMirror, error) {
	accessKey := strings.TrimSpace(os.Getenv("QINIU_ACCESS_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("QINIU_SECRET_KEY"))
	bucket := strings.TrimSpace(os.Getenv("QINIU_BUCKET"))
	domain := strings.TrimSpace(os.Getenv("QINIU_DOMAIN"))
	if accessKey == "" && secretKey == "" && bucket == "" && domain == "" {
		return nil, nil
	}
	if accessKey == "" || secretKey == "" || bucket == "" || domain == "" {
		return nil, errors.New("QINIU_ACCESS_KEY, QINIU_SECRET_KEY, QINIU_BUCKET and QINIU_DOMAIN must be configured together")
	}
	parsedDomain, err := url.Parse(domain)
	if err != nil || parsedDomain.Scheme != "https" || parsedDomain.Hostname() == "" || parsedDomain.User != nil || (parsedDomain.Path != "" && parsedDomain.Path != "/") || parsedDomain.RawQuery != "" || parsedDomain.Fragment != "" {
		return nil, errors.New("QINIU_DOMAIN must be an https origin")
	}
	return &qiniuThumbnailMirror{
		credentials: auth.New(accessKey, secretKey),
		bucket:      bucket,
		domain:      strings.TrimRight(domain, "/"),
		uploader:    storage.NewFormUploader(&storage.Config{UseHTTPS: true}),
		fetch:       fetchRemoteImage,
	}, nil
}

func (m *qiniuThumbnailMirror) Mirror(ctx context.Context, sourceURL string) (string, error) {
	data, mediaType, err := m.fetch(ctx, sourceURL)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	key := "thumbnails/" + hex.EncodeToString(sum[:]) + imageExtension(mediaType)
	policy := storage.PutPolicy{
		Scope:      m.bucket + ":" + key,
		Expires:    300,
		FsizeLimit: maxThumbnailBytes,
		DetectMime: 1,
		MimeLimit:  "image/jpeg;image/png;image/gif;image/webp",
	}
	uploadToken := policy.UploadToken(m.credentials)
	var result storage.PutRet
	if err := m.uploader.Put(ctx, &result, uploadToken, key, bytes.NewReader(data), int64(len(data)), &storage.PutExtra{MimeType: mediaType, TryTimes: 1}); err != nil {
		return "", fmt.Errorf("upload thumbnail: %w", err)
	}
	return storage.MakePublicURL(m.domain, key), nil
}

func fetchRemoteImage(ctx context.Context, sourceURL string) ([]byte, string, error) {
	if !isPublicURL(sourceURL) {
		return nil, "", errors.New("thumbnail URL is not public")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "csqa-thumbnail-mirror/1.0")
	resp, err := newPublicHTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("thumbnail returned status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxThumbnailBytes {
		return nil, "", errors.New("thumbnail exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxThumbnailBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 || len(data) > maxThumbnailBytes {
		return nil, "", errors.New("thumbnail has invalid size")
	}
	mediaType, err := detectImageMediaType(data)
	if err != nil {
		return nil, "", err
	}
	return data, mediaType, nil
}

func detectImageMediaType(data []byte) (string, error) {
	mediaType := http.DetectContentType(data)
	if imageExtension(mediaType) == "" {
		return "", errors.New("thumbnail content is not a supported image")
	}
	return mediaType, nil
}

func imageExtension(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}
