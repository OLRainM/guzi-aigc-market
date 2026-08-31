package asset

import (
	"bytes"
	"strings"
	"testing"
)

func jpegHead() []byte { return []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10} }
func pngHead() []byte {
	return append(append([]byte{}, pngSignature...), 0x00, 0x00, 0x00, 0x0D)
}
func webpHead() []byte {
	head := make([]byte, 12)
	copy(head[0:4], webpRIFF)
	copy(head[8:12], webpWEBP)
	return head
}
func glbHead() []byte { return append(append([]byte{}, glbMagic...), 0x02, 0x00, 0x00, 0x00) }

func TestInspectFile(t *testing.T) {
	tests := []struct {
		name         string
		kind         string
		filename     string
		declaredMIME string
		head         []byte
		size         int64
		wantMIME     string
		wantExt      string
		wantErr      error
	}{
		{name: "jpeg", kind: KindImage, filename: "cover.JPG", declaredMIME: "image/jpeg", head: jpegHead(), size: 1024, wantMIME: "image/jpeg", wantExt: "jpg"},
		{name: "png", kind: KindImage, filename: "a.png", declaredMIME: "image/png", head: pngHead(), size: 2048, wantMIME: "image/png", wantExt: "png"},
		{name: "webp", kind: KindImage, filename: "a.webp", declaredMIME: "image/webp", head: webpHead(), size: 4096, wantMIME: "image/webp", wantExt: "webp"},
		{name: "glb", kind: KindModel, filename: "model.glb", declaredMIME: "model/gltf-binary", head: glbHead(), size: 12, wantMIME: "model/gltf-binary", wantExt: "glb"},
		{name: "octet-stream glb allowed", kind: KindModel, filename: "model.glb", declaredMIME: "application/octet-stream", head: glbHead(), size: 64, wantMIME: "model/gltf-binary", wantExt: "glb"},
		{name: "empty", kind: KindImage, filename: "a.jpg", head: nil, size: 0, wantErr: ErrInvalidFile},
		{name: "text as image", kind: KindImage, filename: "a.jpg", declaredMIME: "image/jpeg", head: []byte("hello"), size: 5, wantErr: ErrUnsupportedType},
		{name: "extension mismatch", kind: KindImage, filename: "a.png", declaredMIME: "image/jpeg", head: jpegHead(), size: 100, wantErr: ErrUnsupportedType},
		{name: "mime mismatch", kind: KindImage, filename: "a.jpg", declaredMIME: "image/png", head: jpegHead(), size: 100, wantErr: ErrUnsupportedType},
		{name: "kind mismatch", kind: KindImage, filename: "a.glb", declaredMIME: "model/gltf-binary", head: glbHead(), size: 32, wantErr: ErrKindMismatch},
		{name: "image too large", kind: KindImage, filename: "a.jpg", declaredMIME: "image/jpeg", head: jpegHead(), size: MaxImageBytes + 1, wantErr: ErrFileTooLarge},
		{name: "glb too large", kind: KindModel, filename: "a.glb", declaredMIME: "model/gltf-binary", head: glbHead(), size: MaxModelBytes + 1, wantErr: ErrFileTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, mime, ext, err := inspectFile(test.kind, test.filename, test.declaredMIME, test.head, test.size)
			if test.wantErr != nil {
				if err != test.wantErr {
					t.Fatalf("error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mime != test.wantMIME || ext != test.wantExt {
				t.Fatalf("mime=%s ext=%s, want mime=%s ext=%s", mime, ext, test.wantMIME, test.wantExt)
			}
		})
	}
}

func TestObjectKey(t *testing.T) {
	key := objectKey("user-1", "product-1", "asset-1", "jpg")
	if key != "products/user-1/product-1/asset-1.jpg" {
		t.Fatalf("unexpected key %q", key)
	}
	if strings.Contains(key, "..") {
		t.Fatal("object key must not contain path traversal")
	}
}

func TestMemoryStoreRoundTrip(t *testing.T) {
	store := NewMemoryStore("aigc-assets")
	payload := []byte("glTF-test-body")
	if err := store.Put(t.Context(), "k.glb", "model/gltf-binary", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	body, info, err := store.Get(t.Context(), "k.glb")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got := bytes.NewBuffer(nil)
	_, _ = got.ReadFrom(body)
	if got.String() != string(payload) || info.Size != int64(len(payload)) {
		t.Fatalf("got %q size %d", got.String(), info.Size)
	}
}
