package invoice_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"hesapkitap/internal/invoice"
)

func TestProviderStatusClassification(t *testing.T) {
	tests := []struct {
		status                          invoice.ProviderStatus
		final, success, failure, ledger bool
	}{
		{invoice.StatusProcessing, false, false, false, true},
		{invoice.StatusPreparingDocument, false, false, false, true},
		{invoice.StatusDocumentError, true, false, true, false},
		{invoice.StatusCreated, false, false, false, true},
		{invoice.StatusSentToGIB, false, false, false, true},
		{invoice.StatusAwaitingReply, false, false, false, true},
		{invoice.StatusRejecting, false, false, false, true},
		{invoice.StatusRejected, true, false, true, false},
		{invoice.StatusApproving, false, false, false, true},
		{invoice.StatusApproved, true, true, false, true},
		{invoice.StatusCancelled, true, false, false, false},
		{invoice.StatusFailed, true, false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			assert.True(t, tt.status.Known())
			assert.Equal(t, tt.final, tt.status.IsFinal(), "IsFinal")
			assert.Equal(t, tt.success, tt.status.IsSuccess(), "IsSuccess")
			assert.Equal(t, tt.failure, tt.status.IsFailure(), "IsFailure")
			assert.Equal(t, tt.ledger, tt.status.CountsTowardsLedger(), "CountsTowardsLedger")
		})
	}
}

func TestUnknownProviderStatusIsNotFinal(t *testing.T) {
	s := invoice.ProviderStatus(999)
	assert.False(t, s.Known())
	assert.False(t, s.IsFinal(), "bilinmeyen kod nihai sayılmaz; polling devam eder")
	assert.Equal(t, "Bilinmeyen durum (999)", s.String())
}
