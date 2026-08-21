// Package application coordinates file metadata and blob storage.
package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/eagle-go/eagle/internal/modules/file/domain"
)

type Usecase struct {
	repo    domain.Repository
	blobs   domain.BlobStore
	maxSize int64
}

func NewUsecase(repo domain.Repository, blobs domain.BlobStore, maxSize int64) *Usecase {
	return &Usecase{repo: repo, blobs: blobs, maxSize: maxSize}
}

func (uc *Usecase) Upload(ctx context.Context, owner, name, contentType string, content []byte) (*domain.File, error) {
	if int64(len(content)) > uc.maxSize {
		return nil, domain.ErrFileTooLarge
	}
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(content)
	file, err := domain.NewFile(id, owner, name, contentType, int64(len(content)), hex.EncodeToString(hash[:]))
	if err != nil {
		return nil, err
	}
	if err := uc.blobs.Put(ctx, file.StorageKey, content); err != nil {
		return nil, err
	}
	created, err := uc.repo.Create(ctx, file)
	if err != nil {
		_ = uc.blobs.Delete(ctx, file.StorageKey)
		return nil, err
	}
	return created, nil
}

func (uc *Usecase) Get(ctx context.Context, owner, id string) (*domain.File, error) {
	return uc.repo.GetOwned(ctx, owner, id)
}

func (uc *Usecase) Download(ctx context.Context, owner, id string) (*domain.File, []byte, error) {
	file, err := uc.repo.GetOwned(ctx, owner, id)
	if err != nil {
		return nil, nil, err
	}
	content, err := uc.blobs.Read(ctx, file.StorageKey)
	if err != nil {
		return nil, nil, err
	}
	return file, content, nil
}

func (uc *Usecase) List(ctx context.Context, q domain.ListQuery) ([]*domain.File, int64, error) {
	return uc.repo.List(ctx, q)
}

func (uc *Usecase) Delete(ctx context.Context, owner, id string) error {
	file, err := uc.repo.GetOwned(ctx, owner, id)
	if err != nil {
		return err
	}
	if err := uc.blobs.Delete(ctx, file.StorageKey); err != nil {
		return err
	}
	return uc.repo.DeleteOwned(ctx, owner, id)
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate file id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
