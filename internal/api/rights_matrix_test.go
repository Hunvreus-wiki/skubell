package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRightsMatrixDeletionAvailability(t *testing.T) {
	t.Parallel()

	withDelete := EvaluateWorkflowAvailability([]string{"delete"})
	require.True(t, withDelete[WorkflowDeletion].Available)

	withoutDelete := EvaluateWorkflowAvailability([]string{"read"})
	require.False(t, withoutDelete[WorkflowDeletion].Available)
	require.Equal(t, []string{"delete"}, withoutDelete[WorkflowDeletion].MissingRights)
}

func TestRightsMatrixHistoryMergeRequirements(t *testing.T) {
	t.Parallel()

	available := EvaluateWorkflowAvailability([]string{"delete", "undelete", "move"})
	require.True(t, available[WorkflowHistoryMerge].Available)

	missing := EvaluateWorkflowAvailability([]string{"delete", "move"})
	require.False(t, missing[WorkflowHistoryMerge].Available)
	require.Equal(t, []string{"undelete"}, missing[WorkflowHistoryMerge].MissingRights)
}

func TestRightsMatrixBlockAuditRequirements(t *testing.T) {
	t.Parallel()

	available := EvaluateWorkflowAvailability([]string{"block"})
	require.True(t, available[WorkflowBlockAudit].Available)
}

func TestRightsMatrixAugeasCoreRequiresRollback(t *testing.T) {
	t.Parallel()

	available := EvaluateWorkflowAvailability([]string{"delete", "block", "rollback"})
	require.True(t, available[WorkflowAugeasCore].Available)

	missing := EvaluateWorkflowAvailability([]string{"delete", "block"})
	require.False(t, missing[WorkflowAugeasCore].Available)
	require.Equal(t, []string{"rollback"}, missing[WorkflowAugeasCore].MissingRights)
}

func TestRightsMatrixProtectionAndUnprotect(t *testing.T) {
	t.Parallel()

	withProtect := EvaluateWorkflowAvailability([]string{"protect"})
	require.True(t, withProtect[WorkflowProtection].Available)

	withoutProtect := EvaluateWorkflowAvailability([]string{"read"})
	require.False(t, withoutProtect[WorkflowProtection].Available)
	require.Equal(t, []string{"protect"}, withoutProtect[WorkflowProtection].MissingRights)
}
