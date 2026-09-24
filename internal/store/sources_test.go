package store_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/CruGlobal/cruorg_proxy/internal/store"
)

type fakeS3 struct {
	etag     string
	gotMatch string
}

func (f *fakeS3) GetObject(
	_ context.Context,
	in *s3.GetObjectInput,
	_ ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	f.gotMatch = aws.ToString(in.IfNoneMatch)
	if f.gotMatch == f.etag {
		return nil, &awshttp.ResponseError{ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusNotModified}},
			Err:      errors.New("not modified"),
		}}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(`{"a":1}`)), ETag: aws.String(f.etag)}, nil
}

func TestS3SourceConditionalGet(t *testing.T) {
	fake := &fakeS3{etag: `"abc"`}
	src := store.S3Source{Client: fake, Bucket: "b", Key: "rules.json"}

	body, tag, err := src.Fetch(context.Background(), "")
	if err != nil || string(body) != `{"a":1}` || tag != `"abc"` {
		t.Fatalf("body %q tag %q err %v", body, tag, err)
	}
	if fake.gotMatch != "" {
		t.Fatalf("first fetch sent If-None-Match %q", fake.gotMatch)
	}
	if _, _, nmErr := src.Fetch(context.Background(), `"abc"`); !errors.Is(nmErr, store.ErrNotModified) {
		t.Fatalf("err %v, want ErrNotModified", nmErr)
	}
}
