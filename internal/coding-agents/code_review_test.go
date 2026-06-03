package codingagents

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reviewerAgent struct {
	stubAgent
	gotReq CodeReviewRequest
	err    error
	pid    int
}

func (r *reviewerAgent) CodeReview(
	_ context.Context, req CodeReviewRequest,
) (int, error) {
	r.gotReq = req
	return r.pid, r.err
}

func TestRunCodeReview_Forwards(t *testing.T) {
	r := &reviewerAgent{pid: 42}
	pid, err := RunCodeReview(t.Context(), r, CodeReviewRequest{Model: "opus"})
	require.NoError(t, err)
	assert.Equal(t, 42, pid)
	assert.Equal(t, "opus", r.gotReq.Model)
}

func TestRunCodeReview_ErrorPassthrough(t *testing.T) {
	want := errors.New("boom")
	r := &reviewerAgent{err: want}
	_, err := RunCodeReview(t.Context(), r, CodeReviewRequest{})
	require.ErrorIs(t, err, want)
}

func TestRunCodeReview_NotImplemented(t *testing.T) {
	_, err := RunCodeReview(t.Context(), stubAgent{}, CodeReviewRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support code-review")
}
