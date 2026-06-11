package storage

import "context"

type Uploader interface {
	Upload(ctx context.Context, filename string, data []byte, contentType string) (string, error)
	Enabled() bool
}
