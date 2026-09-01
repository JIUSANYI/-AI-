package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/qiniu/go-sdk/v7/auth"
	"github.com/qiniu/go-sdk/v7/storage"
)

type fakeQiniuUploader struct {
	key, mediaType string
	size           int64
	err            error
}

func (f *fakeQiniuUploader) Put(_ context.Context, _ interface{}, _, key string, _ io.Reader, size int64, extra *storage.PutExtra) error {
	f.key, f.mediaType, f.size = key, extra.MimeType, size
	return f.err
}

func TestNewThumbnailMirrorFromEnvDisabled(t *testing.T) {
	for _, key := range []string{"QINIU_ACCESS_KEY", "QINIU_SECRET_KEY", "QINIU_BUCKET", "QINIU_DOMAIN"} {
		t.Setenv(key, "")
	}
	mirror, err := newThumbnailMirrorFromEnv()
	if err != nil || mirror != nil {
		t.Fatalf("mirror = %#v, err = %v", mirror, err)
	}
}

func TestNewThumbnailMirrorFromEnvRejectsPartialConfiguration(t *testing.T) {
	t.Setenv("QINIU_ACCESS_KEY", "access")
	t.Setenv("QINIU_SECRET_KEY", "")
	t.Setenv("QINIU_BUCKET", "bucket")
	t.Setenv("QINIU_DOMAIN", "https://cdn.example.com")
	if _, err := newThumbnailMirrorFromEnv(); err == nil {
		t.Fatal("partial Qiniu configuration should fail")
	}
}

func TestNewThumbnailMirrorFromEnvRejectsDomainPath(t *testing.T) {
	t.Setenv("QINIU_ACCESS_KEY", "access")
	t.Setenv("QINIU_SECRET_KEY", "secret")
	t.Setenv("QINIU_BUCKET", "bucket")
	t.Setenv("QINIU_DOMAIN", "https://cdn.example.com/assets")
	if _, err := newThumbnailMirrorFromEnv(); err == nil {
		t.Fatal("Qiniu domain with a path should fail")
	}
}

func TestNewThumbnailMirrorFromEnvRejectsInsecureDomain(t *testing.T) {
	t.Setenv("QINIU_ACCESS_KEY", "access")
	t.Setenv("QINIU_SECRET_KEY", "secret")
	t.Setenv("QINIU_BUCKET", "bucket")
	t.Setenv("QINIU_DOMAIN", "http://cdn.example.com")
	if _, err := newThumbnailMirrorFromEnv(); err == nil {
		t.Fatal("insecure Qiniu domain should fail")
	}
}

func TestQiniuThumbnailMirrorUploadsFetchedImage(t *testing.T) {
	uploader := &fakeQiniuUploader{}
	mirror := &qiniuThumbnailMirror{
		credentials: auth.New("access", "secret"),
		bucket:      "bucket",
		domain:      "https://cdn.example.com",
		uploader:    uploader,
		fetch: func(context.Context, string) ([]byte, string, error) {
			return []byte("png-data"), "image/png", nil
		},
	}
	got, err := mirror.Mirror(context.Background(), "https://images.example.com/a.png")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "https://cdn.example.com/thumbnails/") || !strings.HasSuffix(got, ".png") {
		t.Fatalf("mirrored URL = %q", got)
	}
	if !strings.HasSuffix(uploader.key, ".png") || uploader.mediaType != "image/png" || uploader.size != 8 {
		t.Fatalf("upload = key %q, media type %q, size %d", uploader.key, uploader.mediaType, uploader.size)
	}
}

func TestImageExtensionRejectsActiveContent(t *testing.T) {
	if got := imageExtension("image/svg+xml"); got != "" {
		t.Fatalf("SVG extension = %q, want empty", got)
	}
}

func TestDetectImageMediaType(t *testing.T) {
	png := []byte{'\x89', 'P', 'N', 'G', '\r', '\n', '\x1a', '\n', 0, 0, 0, 0}
	if got, err := detectImageMediaType(png); err != nil || got != "image/png" {
		t.Fatalf("PNG type = %q, err = %v", got, err)
	}
	if _, err := detectImageMediaType([]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)); err == nil {
		t.Fatal("SVG content should be rejected")
	}
}
