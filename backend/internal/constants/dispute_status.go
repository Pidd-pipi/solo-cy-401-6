package constants

// Dispute statuses.
//
// Lifecycle:
//
//	submitted ──(admin accept)──────────────► accepted ──(admin rule)──► ruled(closed)
//	   │            (admin request supplement)   ▲
//	   └──────────────────────────────────────────┴──► awaiting_supplement ──(party supplement)──► accepted
//
// A dispute in {submitted, accepted, awaiting_supplement} is "open": at most one
// open dispute may exist per contract. ruled is terminal and immutable.
const (
	DisputeSubmitted          = "submitted"           // 已提交，待受理
	DisputeAccepted           = "accepted"            // 已受理，处理中
	DisputeAwaitingSupplement = "awaiting_supplement" // 已要求补充材料
	DisputeRuled              = "ruled"               // 已裁决（终态，不可再改）
)

// DisputeOpenStatuses are the non-terminal statuses that count as "in process".
var DisputeOpenStatuses = []string{
	DisputeSubmitted,
	DisputeAccepted,
	DisputeAwaitingSupplement,
}

// ValidDisputeStatus reports whether a status is a valid dispute status.
func ValidDisputeStatus(s string) bool {
	switch s {
	case DisputeSubmitted, DisputeAccepted, DisputeAwaitingSupplement, DisputeRuled:
		return true
	}
	return false
}

// DisputeIsOpen reports whether the status is a non-terminal (open) status.
func DisputeIsOpen(s string) bool {
	return s == DisputeSubmitted || s == DisputeAccepted || s == DisputeAwaitingSupplement
}
