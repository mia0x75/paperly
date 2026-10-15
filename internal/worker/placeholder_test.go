package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// Kinds whose domain layer has not landed are skipped, never left pending.
func TestPlaceholdersSkipWithReasons(t *testing.T) {
	ctx := context.Background()
	err := handleAggregateGapsUnavailable(ctx, testDeps(), &domain.Task{})
	requireSkipped(t, err, skipAggregateGapsReason)
}

// The skip / nonRetryable contracts are stable; the Worker main loop depends on them.
func TestSkipAndNonRetryableSignals(t *testing.T) {
	var sk *errSkipped
	require.True(t, errors.As(skip("why"), &sk))
	require.Equal(t, "why", sk.Error())

	var nre *errNonRetryable
	root := errors.New("base")
	require.True(t, errors.As(nonRetryable(root), &nre))
	require.ErrorIs(t, nre, root, "nonRetryable 可 Unwrap 出原始错误")
	require.Equal(t, root.Error(), nre.Error())
}
