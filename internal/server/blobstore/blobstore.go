// Package blobstore provides streaming object storage for large file blobs.
package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// BlobStore stores and retrieves binary objects without buffering them in
// memory: content is streamed in both directions.
type BlobStore interface {
	// Put streams the content of r into the object identified by key.
	Put(ctx context.Context, key string, r io.Reader) error
	// Get returns a readable stream of the object identified by key.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the object identified by key, if it exists.
	Delete(ctx context.Context, key string) error
}

// S3Config holds the connection parameters of the S3-compatible object store.
type S3Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
}

// S3BlobStore is a BlobStore implementation backed by an S3-compatible object
// storage (MinIO in the local setup). Both Put and Get are fully streaming.
type S3BlobStore struct {
	client *s3.Client
	bucket string
}

// NewS3BlobStore builds an S3BlobStore with static credentials for the given
// endpoint, using path-style addressing which MinIO requires.
func NewS3BlobStore(ctx context.Context, cfg S3Config) (*S3BlobStore, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
	})
	return &S3BlobStore{client: client, bucket: cfg.Bucket}, nil
}

// EnsureBucket creates the configured bucket if it does not exist yet.
func (s *S3BlobStore) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}
	var notFound *types.NotFound
	if !errors.As(err, &notFound) {
		var apiErr interface{ ErrorCode() string }
		if !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NotFound" {
			return fmt.Errorf("head bucket: %w", err)
		}
	}
	_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	if err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}
	return nil
}

// Put stores the content of r into the object identified by key.
// The AWS SDK requires a seekable body over plain HTTP (it computes the
// SigV4 payload hash), so unseekable streams are spilled to a temporary
// file first. The spill goes to disk, never to memory, and is removed
// as soon as the upload completes.
func (s *S3BlobStore) Put(ctx context.Context, key string, r io.Reader) error {
	body, cleanup, err := seekableBody(r)
	defer cleanup()
	if err != nil {
		return fmt.Errorf("prepare object %s: %w", key, err)
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}
	return nil
}

// seekableBody returns a seekable reader for r, spilling unseekable
// streams to a temporary file. The returned cleanup function removes the
// spill file, if any.
func seekableBody(r io.Reader) (io.ReadSeeker, func(), error) {
	if rs, ok := r.(io.ReadSeeker); ok {
		return rs, func() {}, nil
	}
	tmp, err := os.CreateTemp("", "gophkeeper-blob-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, func() {}, fmt.Errorf("spill to temp file: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, func() {}, fmt.Errorf("rewind temp file: %w", err)
	}
	return tmp, func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}, nil
}

// Get returns the object body as a stream.
func (s *S3BlobStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", key, err)
	}
	return out.Body, nil
}

// Delete removes the object if it exists; deleting a missing key is not an error.
func (s *S3BlobStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete object %s: %w", key, err)
	}
	return nil
}
