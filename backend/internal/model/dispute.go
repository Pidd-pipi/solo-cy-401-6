package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// Dispute is a contract dispute filed by either party.
//
// At most one open dispute may exist per contract: OpenContractID mirrors the
// contract id when the dispute is open and is NULL once ruled, backed by a
// unique index (a closed/historical dispute no longer occupies the slot).
type Dispute struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	ContractID     uint   `gorm:"index;not null" json:"contractId"`
	OpenContractID *uint  `gorm:"column:open_contract_id;uniqueIndex;default:null" json:"-"`
	ComplainantID  uint   `gorm:"index;not null" json:"complainantId"`
	Reason         string `gorm:"size:512;not null" json:"reason"`
	Claim          string `gorm:"size:512;not null" json:"claim"`
	EvidenceJS     string `gorm:"column:evidence;type:text" json:"-"`
	Status         string `gorm:"size:24;not null;default:submitted" json:"status"`
	AdminID        *uint  `gorm:"index" json:"adminId"`
	SupplementNote string `gorm:"size:512" json:"supplementNote"`
	// Ruling result (set only once the dispute is ruled).
	Responsibility string     `gorm:"size:512" json:"responsibility"`
	RulingParty    string     `gorm:"size:16" json:"rulingParty"`
	RefundAmount   *float64   `gorm:"type:decimal(14,2)" json:"refundAmount"`
	Opinion        string     `gorm:"size:1024" json:"opinion"`
	RuledAt        *time.Time `json:"ruledAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"-"`

	// Computed fields.
	Evidence    []string            `gorm:"-" json:"evidence"`
	Contract    *Contract           `gorm:"foreignKey:ContractID" json:"contract"`
	Complainant *User               `gorm:"foreignKey:ComplainantID" json:"complainant"`
	Admin       *User               `gorm:"foreignKey:AdminID" json:"admin"`
	Supplements []DisputeSupplement `gorm:"foreignKey:DisputeID" json:"supplements"`
}

// BeforeSave serializes the evidence list.
func (d *Dispute) BeforeSave(_ *gorm.DB) error {
	if d.Evidence != nil {
		raw, err := json.Marshal(d.Evidence)
		if err != nil {
			return err
		}
		d.EvidenceJS = string(raw)
	}
	return nil
}

// AfterFind restores the evidence list.
func (d *Dispute) AfterFind(_ *gorm.DB) error {
	d.Evidence = []string{}
	if d.EvidenceJS != "" {
		_ = json.Unmarshal([]byte(d.EvidenceJS), &d.Evidence)
	}
	return nil
}

// InvolvesUser reports whether the user is a party of the disputed contract.
// Contract must be preloaded.
func (d *Dispute) InvolvesUser(userID uint) bool {
	return d.Contract != nil && (d.Contract.PartyAID == userID || d.Contract.PartyBID == userID)
}
