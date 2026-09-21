package artifacts

import (
	"context"
	"errors"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Config struct {
	Endpoint, Bucket, Region, AccessKey, SecretKey string
	Secure                                         bool
}

type S3 struct {
	client *minio.Client
	bucket string
}

// NewS3 checks an existing bucket. Bucket creation and credential provisioning
// belong to deployment initialization, not individual worker startup.
func NewS3(ctx context.Context, config S3Config) (*S3, error) {
	if config.Endpoint == "" || config.Bucket == "" || config.AccessKey == "" || config.SecretKey == "" {
		return nil, errors.New("S3 endpoint, bucket and credentials are required")
	}
	client, err := minio.New(config.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""), Secure: config.Secure, Region: config.Region})
	if err != nil {
		return nil, err
	}
	exists, err := client.BucketExists(ctx, config.Bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("artifact bucket does not exist; initialize it before starting Execution")
	}
	return &S3{client: client, bucket: config.Bucket}, nil
}

func (store *S3) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	if err := validateKey(key); err != nil {
		return err
	}
	_, err := store.client.PutObject(ctx, store.bucket, key, reader, size, minio.PutObjectOptions{ContentType: "application/octet-stream", SendContentMd5: true})
	return err
}

func (store *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	object, err := store.client.GetObject(ctx, store.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := object.Stat(); err != nil {
		object.Close()
		return nil, err
	}
	return object, nil
}

func (store *S3) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	return store.client.RemoveObject(ctx, store.bucket, key, minio.RemoveObjectOptions{})
}

func (store *S3) SignedURL(ctx context.Context, key string, ttl time.Duration) (*url.URL, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	if ttl <= 0 || ttl > 15*time.Minute {
		return nil, errors.New("artifact URL lifetime must be positive and at most 15 minutes")
	}
	return store.client.PresignedGetObject(ctx, store.bucket, key, ttl, nil)
}
