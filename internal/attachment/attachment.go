// Package attachment stores photos and documents (çek fotoğrafı, teslim fişi, dekont) attached to a record.
package attachment

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Owner is the kind of record a file is attached to.
type Owner string

const (
	OwnerPayment         Owner = "payment"
	OwnerCheque          Owner = "cheque"
	OwnerSupplierEntry   Owner = "supplier_entry"
	OwnerSupplierPayment Owner = "supplier_payment"
)

// Valid reports whether o is a known owner type.
func (o Owner) Valid() bool {
	switch o {
	case OwnerPayment, OwnerCheque, OwnerSupplierEntry, OwnerSupplierPayment:
		return true
	}
	return false
}

// MaxSize is the largest accepted file (phone photos are a few MB).
const MaxSize = 10 << 20

// Meta describes a stored file without its bytes.
type Meta struct {
	ID          uuid.UUID
	OwnerType   Owner
	OwnerID     uuid.UUID
	Filename    string
	ContentType string
	Size        int
	UploadedBy  string
	CreatedAt   time.Time
}

// IsImage reports whether the file can be shown as a picture.
func (m Meta) IsImage() bool { return strings.HasPrefix(m.ContentType, "image/") }

// File is a stored file with its bytes.
type File struct {
	Meta
	Data []byte
}

// Errors returned for bad uploads (messages are shown to the user).
var (
	ErrNotFound    = errors.New("dosya bulunamadı")
	ErrEmpty       = errors.New("dosya boş")
	ErrTooLarge    = errors.New("dosya 10 MB'tan büyük olamaz")
	ErrUnsupported = errors.New("yalnızca fotoğraf (JPEG, PNG, GIF, WebP) ve PDF yüklenebilir")
)

// Detect checks the bytes (not the name the browser claims) and returns the content type.
func Detect(data []byte) (string, error) {
	if len(data) == 0 {
		return "", ErrEmpty
	}
	if len(data) > MaxSize {
		return "", ErrTooLarge
	}
	ct := http.DetectContentType(data)
	switch ct {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "application/pdf":
		return ct, nil
	}
	return "", ErrUnsupported
}

// CleanName keeps only the base name, drops control characters and shortens it.
func CleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > 120 {
		name = string(r[len(r)-120:])
	}
	if name == "" || name == "." || name == "/" {
		return "dosya"
	}
	return name
}

// Store is the persistence port, implemented in internal/platform/db.
type Store interface {
	Add(ctx context.Context, f File) (uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (File, error)
	List(ctx context.Context, owner Owner, ownerID uuid.UUID) ([]Meta, error)
	Delete(ctx context.Context, id uuid.UUID) (Owner, uuid.UUID, error)
	Counts(ctx context.Context, owner Owner, ids []uuid.UUID) (map[uuid.UUID]int, error)
}
