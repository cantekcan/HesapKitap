package incoming_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/incoming"
	"hesapkitap/internal/invoice"
)

// pagedProvider serves a fixed list in pages and counts calls.
type pagedProvider struct {
	items   []incoming.Invoice
	calls   int
	replies int
}

func (p *pagedProvider) Search(_ context.Context, f incoming.Filter) (incoming.Page, error) {
	p.calls++
	var matched []incoming.Invoice
	for _, it := range p.items {
		if f.Scenario != "" && it.Scenario != f.Scenario {
			continue
		}
		matched = append(matched, it)
	}
	pages := (len(matched) + f.Size - 1) / f.Size
	lo := min(f.Page*f.Size, len(matched))
	hi := min(lo+f.Size, len(matched))
	return incoming.Page{Items: matched[lo:hi], Total: len(matched), Pages: pages, Page: f.Page}, nil
}

func (p *pagedProvider) Reply(context.Context, uuid.UUID, invoice.ReplyType, string) error {
	p.replies++
	return nil
}

func (p *pagedProvider) DocumentURL(context.Context, uuid.UUID) (string, error) { return "u", nil }

func commercial(replied bool) incoming.Invoice {
	it := incoming.Invoice{UUID: uuid.New(), Scenario: incoming.ScenarioCommercial}
	if replied {
		it.ReplyType = invoice.ReplyApproved
	}
	return it
}

func TestAwaitingListScansPagesAndKeepsUnanswered(t *testing.T) {
	p := &pagedProvider{}
	for i := 0; i < 250; i++ { // 3 sayfa (100'lük)
		p.items = append(p.items, commercial(i%5 != 0)) // her 5.si yanıtsız → 50
	}
	p.items = append(p.items, incoming.Invoice{UUID: uuid.New(), Scenario: incoming.ScenarioBasic})
	s := &incoming.Service{Provider: p}

	pg, err := s.List(t.Context(), incoming.Filter{}, true)
	require.NoError(t, err)
	assert.Len(t, pg.Items, 50)
	assert.Equal(t, 3, p.calls)

	p.calls = 0
	c, err := s.Counts(t.Context())
	require.NoError(t, err)
	assert.Equal(t, incoming.Counts{All: 251, Unread: 251, AwaitingReply: 50}, c)
	callsAfterFirst := p.calls
	_, err = s.Counts(t.Context())
	require.NoError(t, err)
	assert.Equal(t, callsAfterFirst, p.calls, "sayılar 1 dk önbellekte")
}

func TestAwaitingScanIsBounded(t *testing.T) {
	p := &pagedProvider{}
	for i := 0; i < 700; i++ {
		p.items = append(p.items, commercial(false))
	}
	s := &incoming.Service{Provider: p}
	c, err := s.Counts(t.Context())
	require.NoError(t, err)
	assert.True(t, c.Approximate)
	assert.Equal(t, 500, c.AwaitingReply, "en çok 5 sayfa taranır")
}

func TestReplyValidation(t *testing.T) {
	p := &pagedProvider{}
	s := &incoming.Service{Provider: p}
	id := uuid.New()
	assert.ErrorIs(t, s.Reply(t.Context(), id, invoice.ReplyReturned, "x"), incoming.ErrBadAnswer)
	assert.ErrorIs(t, s.Reply(t.Context(), id, invoice.ReplyApproved, "   "), incoming.ErrNoteNeeded)
	assert.Zero(t, p.replies)
	require.NoError(t, s.Reply(t.Context(), id, invoice.ReplyDenied, "olmaz"))
	assert.Equal(t, 1, p.replies)

	none := &incoming.Service{}
	assert.ErrorIs(t, none.Reply(t.Context(), id, invoice.ReplyApproved, "x"), incoming.ErrNoProvider)
	_, err := none.Counts(t.Context())
	assert.ErrorIs(t, err, incoming.ErrNoProvider)
}
