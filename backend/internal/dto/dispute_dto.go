package dto

// CreateDisputeRequest is the payload for a party filing a contract dispute.
type CreateDisputeRequest struct {
	Reason   string   `json:"reason" validate:"required,min=5,max=500"`
	Claim    string   `json:"claim" validate:"required,min=2,max=500"`
	Evidence []string `json:"evidence" validate:"dive,max=255"`
}

// SupplementRequest is the payload for a party adding material after an
// administrator requests a supplement.
type SupplementRequest struct {
	Content     string   `json:"content" validate:"required,min=2,max=500"`
	Attachments []string `json:"attachments" validate:"dive,max=255"`
}

// RequestSupplementRequest is the administrator payload asking for more material.
type RequestSupplementRequest struct {
	Note string `json:"note" validate:"required,min=2,max=500"`
}

// RuleDisputeRequest is the administrator ruling payload. All four result
// fields are mandatory for a valid ruling.
type RuleDisputeRequest struct {
	// Responsibility is the written division of liability between the parties.
	Responsibility string `json:"responsibility" validate:"required,min=2,max=500"`
	// RulingParty marks which side is held responsible: party_a / party_b / shared.
	RulingParty  string  `json:"rulingParty" validate:"required,oneof=party_a party_b shared"`
	RefundAmount float64 `json:"refundAmount" validate:"min=0"`
	Opinion      string  `json:"opinion" validate:"required,min=2,max=1000"`
}
