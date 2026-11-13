package db

import (
	"context"

	"hesapkitap/internal/audit"
	"hesapkitap/internal/platform/db/dbgen"
)

// AuditStore implements audit.Store.
type AuditStore struct{ q *dbgen.Queries }

func NewAuditStore(q *dbgen.Queries) *AuditStore { return &AuditStore{q: q} }

var _ audit.Store = (*AuditStore)(nil)

func (s *AuditStore) Add(ctx context.Context, e audit.Entry) error {
	return s.q.InsertAudit(ctx, dbgen.InsertAuditParams{Actor: e.Actor, Action: e.Action, Entity: e.Entity, EntityID: e.EntityID, Summary: e.Summary})
}

func (s *AuditStore) List(ctx context.Context, query string, limit, offset int) ([]audit.Entry, int, error) {
	total, err := s.q.CountAudit(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListAudit(ctx, dbgen.ListAuditParams{Q: query, Lim: int32(limit), Off: int32(offset)}) //nolint:gosec // sayfa boyutu sabit ve küçük
	if err != nil {
		return nil, 0, err
	}
	out := make([]audit.Entry, len(rows))
	for i, r := range rows {
		out[i] = audit.Entry{ID: r.ID, At: r.At.Time, Actor: r.Actor, Action: r.Action, Entity: r.Entity, EntityID: r.EntityID, Summary: r.Summary}
	}
	return out, int(total), nil
}
