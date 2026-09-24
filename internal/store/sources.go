package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// GetObjectAPI is the part of the S3 client S3Source needs.
type GetObjectAPI interface {
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// S3Source reads the rules document from one S3 object, using a conditional
// GET so an unchanged object costs no download.
type S3Source struct {
	Client GetObjectAPI
	Bucket string
	Key    string
}

// Fetch implements Source.
func (s S3Source) Fetch(ctx context.Context, etag string) ([]byte, string, error) {
	in := &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(s.Key)}
	if etag != "" {
		in.IfNoneMatch = aws.String(etag)
	}
	out, err := s.Client.GetObject(ctx, in)
	if err != nil {
		var re *awshttp.ResponseError
		if errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotModified {
			return nil, etag, ErrNotModified
		}
		return nil, "", fmt.Errorf("get s3://%s/%s: %w", s.Bucket, s.Key, err)
	}
	defer out.Body.Close()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read s3://%s/%s: %w", s.Bucket, s.Key, err)
	}
	return body, aws.ToString(out.ETag), nil
}

// FileSource reads the rules document from a local file. It is for local runs
// and the parity harness; the version tag is the file's size and mtime.
type FileSource struct {
	Path string
}

// Fetch implements Source.
func (f FileSource) Fetch(_ context.Context, etag string) ([]byte, string, error) {
	info, err := os.Stat(f.Path)
	if err != nil {
		return nil, "", err
	}
	tag := strconv.FormatInt(info.Size(), 10) + "-" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	if tag == etag {
		return nil, etag, ErrNotModified
	}
	body, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, "", err
	}
	return body, tag, nil
}
