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

// Revision visibility is any-of: a regular admin (deleterevision) and a non-admin suppressor (suppressrevision)
// must each get the workflow; only a user with neither right is turned away, told either right would unlock it.
func TestRightsMatrixRevisionDeleteAnyOf(t *testing.T) {
	t.Parallel()

	admin := EvaluateWorkflowAvailability([]string{"deleterevision"})
	require.True(t, admin[WorkflowRevisionDelete].Available)

	suppressor := EvaluateWorkflowAvailability([]string{"suppressrevision"})
	require.True(t, suppressor[WorkflowRevisionDelete].Available)
	require.Empty(t, suppressor[WorkflowRevisionDelete].MissingRights)

	neither := EvaluateWorkflowAvailability([]string{"read"})
	require.False(t, neither[WorkflowRevisionDelete].Available)
	require.Equal(t, []string{"deleterevision", "suppressrevision"}, neither[WorkflowRevisionDelete].MissingRights)

	// The dedicated suppression workflow still strictly requires suppressrevision.
	require.False(t, admin[WorkflowSuppression].Available)
	require.True(t, suppressor[WorkflowSuppression].Available)
}

func TestRightsMatrixProtectionAndUnprotect(t *testing.T) {
	t.Parallel()

	withProtect := EvaluateWorkflowAvailability([]string{"protect"})
	require.True(t, withProtect[WorkflowProtection].Available)

	withoutProtect := EvaluateWorkflowAvailability([]string{"read"})
	require.False(t, withoutProtect[WorkflowProtection].Available)
	require.Equal(t, []string{"protect"}, withoutProtect[WorkflowProtection].MissingRights)
}
