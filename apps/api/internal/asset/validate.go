package asset

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
)

const (
	KindImage = "IMAGE"
	KindModel = "MODEL"

	MaxImageBytes int64 = 10 << 20
	MaxModelBytes int64 = 20 << 20
)

var (
	ErrInvalidFile     = errors.New("invalid file")
	ErrFileTooLarge    = errors.New("file too large")
	ErrUnsupportedType = errors.New("unsupported type")
	ErrKindMismatch    = errors.New("file kind mismatch")
	pngSignature       = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	webpRIFF           = []byte("RIFF")
	webpWEBP           = []byte("WEBP")
	glbMagic           = []byte("glTF")
)

type sniffedFile struct {
	Kind string
	MIME string
	Ext  string
}

func inspectFile(expectedKind, filename, declaredMIME string, head []byte, size int64) (kind, mime, ext string, err error) {
	if size <= 0 || len(head) == 0 {
		return "", "", "", ErrInvalidFile
	}
	detected := sniff(head)
	if detected == nil {
		return "", "", "", ErrUnsupportedType
	}
	if expectedKind != "" && detected.Kind != expectedKind {
		return "", "", "", ErrKindMismatch
	}
	filenameExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	if filenameExt != "" && !extensionMatches(filenameExt, detected.Ext) {
		return "", "", "", ErrUnsupportedType
	}
	if declared := normalizedMIME(declaredMIME); declared != "" && declared != "application/octet-stream" && declared != detected.MIME {
		return "", "", "", ErrUnsupportedType
	}
	limit := MaxImageBytes
	if detected.Kind == KindModel {
		limit = MaxModelBytes
	}
	if size > limit {
		return "", "", "", ErrFileTooLarge
	}
	ext = detected.Ext
	if ext == "jpeg" {
		ext = "jpg"
	}
	return detected.Kind, detected.MIME, ext, nil
}

func sniff(head []byte) *sniffedFile {
	if len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF {
		return &sniffedFile{Kind: KindImage, MIME: "image/jpeg", Ext: "jpg"}
	}
	if bytes.HasPrefix(head, pngSignature) {
		return &sniffedFile{Kind: KindImage, MIME: "image/png", Ext: "png"}
	}
	if len(head) >= 12 && bytes.Equal(head[:4], webpRIFF) && bytes.Equal(head[8:12], webpWEBP) {
		return &sniffedFile{Kind: KindImage, MIME: "image/webp", Ext: "webp"}
	}
	if bytes.HasPrefix(head, glbMagic) {
		return &sniffedFile{Kind: KindModel, MIME: "model/gltf-binary", Ext: "glb"}
	}
	return nil
}

func extensionMatches(actual, detected string) bool {
	if actual == detected {
		return true
	}
	return (actual == "jpeg" && detected == "jpg") || (actual == "jpg" && detected == "jpeg")
}

func normalizedMIME(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if index := strings.Index(value, ";"); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	return value
}

func objectKey(ownerID, productID, assetID, ext string) string {
	return "products/" + ownerID + "/" + productID + "/" + assetID + "." + ext
}

func extensionFromKey(key, fallback string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(key), "."))
	if ext == "" {
		return fallback
	}
	if ext == "jpeg" {
		return "jpg"
	}
	return ext
}
