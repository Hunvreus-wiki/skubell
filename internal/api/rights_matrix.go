package api

import "sort"

const (
	WorkflowDeletion       = "deletion"
	WorkflowRestoration    = "restoration"
	WorkflowBlocking       = "blocking"
	WorkflowBlockAudit     = "block_audit"
	WorkflowProtection     = "protection"
	WorkflowRevisionDelete = "revision_delete"
	WorkflowSuppression    = "suppression"
	WorkflowHistoryMerge   = "history_merge"
	WorkflowUserRights     = "user_rights"
	WorkflowAugeasCore     = "augeas_core"
	WorkflowAugeasRevDel   = "augeas_revision_delete"
)

// WorkflowAvailability describes whether a workflow is usable and which rights are missing.
type WorkflowAvailability struct {
	Available     bool
	MissingRights []string
}

var workflowRequiredRights = map[string][]string{
	WorkflowDeletion:       {"delete"},
	WorkflowRestoration:    {"undelete", "browsearchive"},
	WorkflowBlocking:       {"block"},
	WorkflowBlockAudit:     {"block"},
	WorkflowProtection:     {"protect"},
	WorkflowRevisionDelete: {"deleterevision"},
	WorkflowSuppression:    {"suppressrevision"},
	WorkflowHistoryMerge:   {"delete", "undelete", "move"},
	WorkflowUserRights:     {"userrights"},
	WorkflowAugeasCore:     {"delete", "block", "rollback"},
	WorkflowAugeasRevDel:   {"deleterevision"},
}

// EvaluateWorkflowAvailability maps rights to workflow availability.
func EvaluateWorkflowAvailability(rights []string) map[string]WorkflowAvailability {
	rightSet := make(map[string]struct{}, len(rights))
	for _, right := range rights {
		rightSet[right] = struct{}{}
	}

	result := map[string]WorkflowAvailability{}
	for workflow, required := range workflowRequiredRights {
		missing := []string{}
		for _, right := range required {
			if _, ok := rightSet[right]; !ok {
				missing = append(missing, right)
			}
		}
		sort.Strings(missing)
		result[workflow] = WorkflowAvailability{
			Available:     len(missing) == 0,
			MissingRights: missing,
		}
	}
	return result
}
