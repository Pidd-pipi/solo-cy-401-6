package constants

// Dispute business error codes. They are distinct from the generic codes so the
// caller can tell apart every rejection reason of the dispute workflow.
const (
	// Only the two contract parties may file or supplement a dispute.
	CodeDisputeNotParty = 40310
	// Only an administrator may accept / request supplement / rule.
	CodeDisputeNotAdmin = 40311
	// The contract already has one dispute in process; a closed one is not a bar.
	CodeDisputeAlreadyOpen = 40910
	// The requested state transition is not legal from the current status
	// (e.g. ruling before accepting, accepting after the dispute is closed).
	CodeDisputeIllegalTransition = 40911
	// The dispute is already closed (ruled) and may never be modified again.
	CodeDisputeClosed = 40912
	// A concurrent transaction already changed the dispute (lost-update guard).
	CodeDisputeConcurrent = 40913
)
