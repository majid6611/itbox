// Package s3client is a copy of the core backend's own package of the same
// name — kept duplicated rather than shared, since this module is a
// separate deployable binary with its own go.mod (see the module's design
// notes: modules are independent, self-contained units). Talks to the
// s3-storage module's Garage bucket directly, for per-request wiki
// attachment upload/download.
package s3client

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
	s3     *s3.Client
	bucket string
}

// New builds a client for one bucket. endpoint is the internal
// container:port address (e.g. our own Garage instance) — path-style
// addressing is forced on since that's what Garage (and most
// self-hosted S3-compatible services) requires.
func New(endpoint, accessKey, secretKey, bucket string) *Client {
	cfg := aws.Config{
		Region:      "garage",
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	return &Client{s3: client, bucket: bucket}
}

func (c *Client) Upload(ctx context.Context, key string, body io.Reader, contentType string) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	return err
}

func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, string, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, "", err
	}
	contentType := ""
	if out.ContentType != nil {
		contentType = *out.ContentType
	}
	return out.Body, contentType, nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	return err
}
