package gcpgcs

import (
	"context"
	"fmt"
	"io"
	"os"

	"cloud.google.com/go/storage"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	"github.com/rs/zerolog"
)

type GoogleCloudStorageBackupService struct {
	logger zerolog.Logger
	config types.Config
	bucket string
	path   string

	client *storage.Client
}

func (b GoogleCloudStorageBackupService) Init() error {

	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}

	b.client = client
	return nil
}

func (b GoogleCloudStorageBackupService) LoadFile(path string) ([]byte, error) {
	bkt := b.client.Bucket(b.bucket)
	obj := bkt.Object(b.path + path)

	r, err := obj.NewReader(context.Background())
	if err != nil {
		return nil, err
	}

	defer r.Close()

	content, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return content, nil
}

func (b GoogleCloudStorageBackupService) DownloadFile(remotePath string, localPath string) error {
	bkt := b.client.Bucket(b.bucket)
	obj := bkt.Object(b.path + remotePath)

	// Read it back.
	r, err := obj.NewReader(context.Background())
	if err != nil {
		return err
	}

	defer r.Close()

	out, err := os.Create(localPath)
	if err != nil {
		return err
	}

	defer out.Close()

	if _, err := io.Copy(out, r); err != nil {
		return err
	}

	return nil
}

func (b GoogleCloudStorageBackupService) UploadFile(localPath string, remotePath string, overwrite bool) error {
	bkt := b.client.Bucket(b.bucket)
	obj := bkt.Object(b.path + remotePath)

	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if overwrite {
		attrs, err := obj.Attrs(context.Background())
		if err != nil {
			return fmt.Errorf("object.Attrs: %w", err)
		}
		obj = obj.If(storage.Conditions{GenerationMatch: attrs.Generation})

		if err := obj.Delete(context.Background()); err != nil {
			return fmt.Errorf("Object(%q).Delete: %w", obj.ObjectName(), err)
		}
	} else {
		obj = obj.If(storage.Conditions{DoesNotExist: true})
	}

	wc := obj.NewWriter(context.Background())
	if _, err = io.Copy(wc, f); err != nil {
		return err
	}

	if err := wc.Close(); err != nil {
		return err
	}

	return nil
}

func NewGoogleCloudStorageBackupService(
	logger zerolog.Logger,
	config types.Config,
	bucket string,
	path string,
) (GoogleCloudStorageBackupService, error) {

	service := GoogleCloudStorageBackupService{
		logger: logger,
		config: config,
		bucket: bucket,
		path:   path,
	}

	return service, nil
}
