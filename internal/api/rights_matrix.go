package api

import (
	"slices"
	"sort"
)

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

// workflowAnyOfRights lists workflows usable with ANY one of the listed rights, overriding the all-required matrix
// above. Revision visibility serves regular admins (deleterevision) and non-admin suppressors (suppressrevision)
// alike, so either right makes the workflow available; the UI then gates each control by its specific right.
var workflowAnyOfRights = map[string][]string{
	WorkflowRevisionDelete: {"deleterevision", "suppressrevision"},
}

// EvaluateWorkflowAvailability maps rights to workflow availability.
func EvaluateWorkflowAvailability(rights []string) map[string]WorkflowAvailability {
	rightSet := make(map[string]struct{}, len(rights))
	for _, right := range rights {
		rightSet[right] = struct{}{}
	}

	result := map[string]WorkflowAvailability{}
	for workflow, required := range workflowRequiredRights {
		if anyOf, ok := workflowAnyOfRights[workflow]; ok {
			result[workflow] = anyOfAvailability(rightSet, anyOf)
			continue
		}
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

// anyOfAvailability reports a workflow available when at least one of the alternative rights is held; when none is,
// every alternative is listed as missing (any single one of them would unlock the workflow).
func anyOfAvailability(rightSet map[string]struct{}, anyOf []string) WorkflowAvailability {
	for _, right := range anyOf {
		if _, ok := rightSet[right]; ok {
			return WorkflowAvailability{Available: true, MissingRights: []string{}}
		}
	}
	missing := slices.Clone(anyOf)
	sort.Strings(missing)
	return WorkflowAvailability{Available: false, MissingRights: missing}
}
