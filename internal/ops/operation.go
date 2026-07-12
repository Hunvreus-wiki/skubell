package ops

// OpType identifies the operation type in domain vocabulary.
type OpType string

const (
	OpDeletePage       OpType = "delete_page"
	OpUndeletePage     OpType = "undelete_page"
	OpMovePage         OpType = "move_page"
	OpBlockUser        OpType = "block_user"
	OpUnblockUser      OpType = "unblock_user"
	OpProtectPage      OpType = "protect_page"
	OpRevisionDelete   OpType = "revision_delete"
	OpSuppress         OpType = "suppress"
	OpSetUserRights    OpType = "set_user_rights"
	OpQueryRevisions   OpType = "query_revisions"
	OpQueryPageInfo    OpType = "query_page_info"
	OpQueryBlocks      OpType = "query_blocks"
	OpQueryDeletedRevs OpType = "query_deleted_revisions"
	OpQueryUserInfo    OpType = "query_user_info"
	OpQuerySiteInfo    OpType = "query_site_info"
)

// Operation represents a single semantic operation.
type Operation struct {
	Type        OpType
	Params      map[string]string
	Description string
}

// ExecutionPlan is the complete sequence of operations for a task.
type ExecutionPlan struct {
	Module     string
	Operations []Operation
	ReadPhase  int
}
