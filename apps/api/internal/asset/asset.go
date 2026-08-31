package asset

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const StatusReady = "READY"

type Asset struct {
	ID           string    `gorm:"type:char(36);primaryKey" json:"id"`
	OwnerID      string    `gorm:"type:char(36);not null;index" json:"owner_id"`
	Kind         string    `gorm:"size:16;not null;index" json:"kind"`
	OriginalName string    `gorm:"size:255;not null" json:"original_name"`
	ObjectKey    string    `gorm:"size:512;not null;uniqueIndex" json:"-"`
	Bucket       string    `gorm:"size:128;not null" json:"-"`
	MIMEType     string    `gorm:"size:128;not null" json:"mime_type"`
	SizeBytes    int64     `gorm:"not null" json:"size_bytes"`
	SHA256       string    `gorm:"type:char(64);not null" json:"sha256"`
	Status       string    `gorm:"size:16;not null;default:READY;index" json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Asset) TableName() string { return "assets" }

type Public struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	MIMEType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	OriginalName string `json:"original_name"`
	ContentURL   string `json:"content_url"`
}

type Service struct {
	db     *gorm.DB
	store  ObjectStore
	bucket string
}

func New(db *gorm.DB, store ObjectStore, bucket string) (*Service, error) {
	if bucket == "" {
		bucket = "aigc-assets"
	}
	if err := db.AutoMigrate(&Asset{}); err != nil {
		return nil, err
	}
	return &Service{db: db, store: store, bucket: bucket}, nil
}

func (s *Service) Put(ctx context.Context, ownerID, productID, expectedKind, filename, declaredMIME string, body io.Reader, size int64) (*Asset, error) {
	if err := s.store.EnsureBucket(ctx); err != nil {
		return nil, err
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(body, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	head = head[:n]
	kind, mime, ext, err := inspectFile(expectedKind, filename, declaredMIME, head, size)
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	key := objectKey(ownerID, productID, id, ext)
	hasher := sha256.New()
	reader := io.TeeReader(io.MultiReader(bytes.NewReader(head), body), hasher)
	if err := s.store.Put(ctx, key, mime, reader, size); err != nil {
		return nil, err
	}
	asset := Asset{
		ID: id, OwnerID: ownerID, Kind: kind, OriginalName: filename, ObjectKey: key,
		Bucket: s.bucket, MIMEType: mime, SizeBytes: size, SHA256: hex.EncodeToString(hasher.Sum(nil)), Status: StatusReady,
	}
	if err := s.db.WithContext(ctx).Create(&asset).Error; err != nil {
		_ = s.store.Delete(ctx, key)
		return nil, err
	}
	return &asset, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Asset, error) {
	var asset Asset
	if err := s.db.WithContext(ctx).First(&asset, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

func (s *Service) Open(ctx context.Context, id string) (io.ReadCloser, ObjectInfo, *Asset, error) {
	asset, err := s.Get(ctx, id)
	if err != nil {
		return nil, ObjectInfo{}, nil, err
	}
	body, info, err := s.store.Get(ctx, asset.ObjectKey)
	if err != nil {
		return nil, ObjectInfo{}, nil, err
	}
	if info.ContentType == "" {
		info.ContentType = asset.MIMEType
	}
	if info.Size == 0 {
		info.Size = asset.SizeBytes
	}
	return body, info, asset, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	asset, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&Asset{}, "id = ?", id).Error; err != nil {
		return err
	}
	return s.store.Delete(ctx, asset.ObjectKey)
}

func (a Asset) Public(productID string) Public {
	return Public{
		ID: a.ID, Kind: a.Kind, MIMEType: a.MIMEType, SizeBytes: a.SizeBytes, SHA256: a.SHA256,
		OriginalName: a.OriginalName, ContentURL: "/api/v1/products/" + productID + "/assets/" + a.ID + "/content",
	}
}
