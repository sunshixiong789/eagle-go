// Package domain contains the file module's metadata model and storage ports.
package domain

import (
	"context"
	"errors"
	"mime"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrFileNotFound       = errors.New("file: 文件不存在")
	ErrFileTooLarge       = errors.New("file: 文件超过大小限制")
	ErrInvalidFileName    = errors.New("file: 文件名不合法")
	ErrInvalidContentType = errors.New("file: 内容类型不合法")
)

// File is immutable metadata for one stored blob.
type File struct {
	ID           string
	OwnerSubject string
	Name         string
	StorageKey   string
	ContentType  string
	Size         int64
	SHA256       string
	CreatedAt    time.Time
}

func NewFile(id, owner, name, contentType string, size int64, sha256 string) (*File, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || utf8.RuneCountInString(name) > 255 ||
		filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return nil, ErrInvalidFileName
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if len(contentType) > 128 {
		return nil, ErrInvalidContentType
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, ErrInvalidContentType
	}
	contentType = mime.FormatMediaType(mediaType, params)
	if contentType == "" {
		return nil, ErrInvalidContentType
	}
	return &File{
		ID: id, OwnerSubject: owner, Name: name, StorageKey: id,
		ContentType: contentType, Size: size, SHA256: sha256,
	}, nil
}

type ListQuery struct {
	OwnerSubject string
	Offset       int32
	PageSize     int32
}

type Repository interface {
	Create(context.Context, *File) (*File, error)
	GetOwned(context.Context, string, string) (*File, error)
	List(context.Context, ListQuery) ([]*File, int64, error)
	DeleteOwned(context.Context, string, string) error
}

type BlobStore interface {
	Put(context.Context, string, []byte) error
	Read(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}
