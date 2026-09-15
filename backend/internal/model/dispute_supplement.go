package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// DisputeSupplement records one round of additional material a party submits
// after an administrator requested it.
type DisputeSupplement struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	DisputeID     uint      `gorm:"index;not null" json:"disputeId"`
	SubmitterID   uint      `gorm:"not null" json:"submitterId"`
	Content       string    `gorm:"size:512;not null" json:"content"`
	AttachmentsJS string    `gorm:"column:attachments;type:text" json:"-"`
	CreatedAt     time.Time `json:"createdAt"`

	// Computed fields.
	Attachments []string `gorm:"-" json:"attachments"`
	Submitter   *User    `gorm:"foreignKey:SubmitterID" json:"submitter"`
}

// BeforeSave serializes the attachment list.
func (s *DisputeSupplement) BeforeSave(_ *gorm.DB) error {
	if s.Attachments != nil {
		raw, err := json.Marshal(s.Attachments)
		if err != nil {
			return err
		}
		s.AttachmentsJS = string(raw)
	}
	return nil
}

// AfterFind restores the attachment list.
func (s *DisputeSupplement) AfterFind(_ *gorm.DB) error {
	s.Attachments = []string{}
	if s.AttachmentsJS != "" {
		_ = json.Unmarshal([]byte(s.AttachmentsJS), &s.Attachments)
	}
	return nil
}
