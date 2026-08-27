package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordStore_GetPlanCommentRecord_MissingIsNotAnError(t *testing.T) {
	s, _ := newTestRecordStore(t)
	rec, err := s.GetPlanCommentRecord(context.Background(), "owner", "repo", 42)
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestRecordStore_SetAndGetPlanCommentRecord(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()

	require.NoError(t, s.SetPlanCommentRecord(ctx, "owner", "repo", 42, []string{"node-1", "node-2"}))

	rec, err := s.GetPlanCommentRecord(ctx, "owner", "repo", 42)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, []string{"node-1", "node-2"}, rec.NodeIDs)
}

func TestRecordStore_SetPlanCommentRecord_ResetsTTL(t *testing.T) {
	s, client := newTestRecordStore(t)
	ctx := context.Background()
	key := planCommentKey("owner", "repo", 42)

	require.NoError(t, s.SetPlanCommentRecord(ctx, "owner", "repo", 42, []string{"node-1"}))
	firstTTL, err := client.TTL(ctx, key).Result()
	require.NoError(t, err)

	require.NoError(t, s.SetPlanCommentRecord(ctx, "owner", "repo", 42, []string{"node-1", "node-2"}))
	secondTTL, err := client.TTL(ctx, key).Result()
	require.NoError(t, err)

	assert.InDelta(t, firstTTL.Seconds(), secondTTL.Seconds(), 2)
}

func TestRecordStore_DeletePlanCommentRecord(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	require.NoError(t, s.SetPlanCommentRecord(ctx, "owner", "repo", 42, []string{"node-1"}))

	require.NoError(t, s.DeletePlanCommentRecord(ctx, "owner", "repo", 42))

	rec, err := s.GetPlanCommentRecord(ctx, "owner", "repo", 42)
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestRecordStore_DeletePlanCommentRecord_MissingIsNoop(t *testing.T) {
	s, _ := newTestRecordStore(t)
	require.NoError(t, s.DeletePlanCommentRecord(context.Background(), "owner", "repo", 42))
}
