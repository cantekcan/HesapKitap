package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hesapkitap/internal/attachment"
	"hesapkitap/internal/platform/db/dbgen"
)

// AttachmentStore implements attachment.Store.
type AttachmentStore struct{ q *dbgen.Queries }

func NewAttachmentStore(q *dbgen.Queries) *AttachmentStore { return &AttachmentStore{q: q} }

var _ attachment.Store = (*AttachmentStore)(nil)

func (s *AttachmentStore) Add(ctx context.Context, f attachment.File) (uuid.UUID, error) {
	return s.q.CreateAttachment(ctx, dbgen.CreateAttachmentParams{OwnerType: string(f.OwnerType), OwnerID: f.OwnerID, Filename: f.Filename,
		ContentType: f.ContentType, Size: int32(f.Size), Data: f.Data, UploadedBy: f.UploadedBy}) //nolint:gosec // boyut MaxSize ile sınırlı
}

func (s *AttachmentStore) Get(ctx context.Context, id uuid.UUID) (attachment.File, error) {
	r, err := s.q.GetAttachment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return attachment.File{}, attachment.ErrNotFound
	}
	if err != nil {
		return attachment.File{}, err
	}
	return attachment.File{Meta: attachment.Meta{ID: r.ID, OwnerType: attachment.Owner(r.OwnerType), OwnerID: r.OwnerID, Filename: r.Filename,
		ContentType: r.ContentType, Size: int(r.Size), UploadedBy: r.UploadedBy, CreatedAt: r.CreatedAt.Time}, Data: r.Data}, nil
}

func (s *AttachmentStore) List(ctx context.Context, owner attachment.Owner, ownerID uuid.UUID) ([]attachment.Meta, error) {
	rows, err := s.q.ListAttachments(ctx, dbgen.ListAttachmentsParams{OwnerType: string(owner), OwnerID: ownerID})
	if err != nil {
		return nil, err
	}
	out := make([]attachment.Meta, len(rows))
	for i, r := range rows {
		out[i] = attachment.Meta{ID: r.ID, OwnerType: attachment.Owner(r.OwnerType), OwnerID: r.OwnerID, Filename: r.Filename,
			ContentType: r.ContentType, Size: int(r.Size), UploadedBy: r.UploadedBy, CreatedAt: r.CreatedAt.Time}
	}
	return out, nil
}

func (s *AttachmentStore) Delete(ctx context.Context, id uuid.UUID) (attachment.Owner, uuid.UUID, error) {
	r, err := s.q.DeleteAttachment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", uuid.Nil, attachment.ErrNotFound
	}
	if err != nil {
		return "", uuid.Nil, err
	}
	return attachment.Owner(r.OwnerType), r.OwnerID, nil
}

func (s *AttachmentStore) Counts(ctx context.Context, owner attachment.Owner, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.q.CountAttachments(ctx, dbgen.CountAttachmentsParams{OwnerType: string(owner), OwnerIds: ids})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.OwnerID] = int(r.N)
	}
	return out, nil
}
