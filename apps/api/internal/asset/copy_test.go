package asset

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func setupAssetService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(db, NewMemoryStore("aigc-assets"), "aigc-assets")
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestCopyModelCreatesIndependentObject(t *testing.T) {
	service := setupAssetService(t)
	ownerID := uuid.NewString()
	jobID := uuid.NewString()
	productID := uuid.NewString()
	body := glbHead()
	source, err := service.Put(context.Background(), ownerID, jobID, KindModel, "model.glb", "model/gltf-binary", bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	copied, err := service.Copy(context.Background(), source.ID, ownerID, productID)
	if err != nil {
		t.Fatal(err)
	}
	if copied.ID == source.ID || copied.ObjectKey == source.ObjectKey {
		t.Fatalf("copy should be independent: source=%+v copied=%+v", source, copied)
	}
	if copied.OwnerID != ownerID || copied.Kind != KindModel || copied.SHA256 != source.SHA256 {
		t.Fatalf("unexpected copy metadata: %+v", copied)
	}
	if err := service.Delete(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	reader, info, stored, err := service.Open(context.Background(), copied.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, _ := io.ReadAll(reader)
	if !bytes.Equal(got, body) || info.Size != int64(len(body)) || stored.ID != copied.ID {
		t.Fatalf("copied content mismatch: size=%d stored=%s", info.Size, stored.ID)
	}
}

func TestCopyRejectsOtherOwner(t *testing.T) {
	service := setupAssetService(t)
	ownerID := uuid.NewString()
	body := glbHead()
	source, err := service.Put(context.Background(), ownerID, uuid.NewString(), KindModel, "model.glb", "model/gltf-binary", bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Copy(context.Background(), source.ID, uuid.NewString(), uuid.NewString()); err == nil {
		t.Fatal("expected copy from another owner to fail")
	}
}
