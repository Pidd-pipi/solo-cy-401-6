package repository

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/model"
)

// DisputeRepository persists disputes and their supplement rounds.
type DisputeRepository struct {
	db *gorm.DB
}

// NewDisputeRepository builds a DisputeRepository.
func NewDisputeRepository(db *gorm.DB) *DisputeRepository {
	return &DisputeRepository{db: db}
}

// tx returns the given transaction, or the repository handle when tx is nil.
func (r *DisputeRepository) tx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// Create inserts a dispute. A duplicate open-dispute unique-index violation is
// reported as ErrDuplicate so the service can return a distinct reason.
func (r *DisputeRepository) Create(tx *gorm.DB, d *model.Dispute) error {
	if err := r.tx(tx).Create(d).Error; err != nil {
		if isDuplicateKeyErr(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("create dispute: %w", err)
	}
	return nil
}

// FindByID loads a dispute with its associations.
func (r *DisputeRepository) FindByID(tx *gorm.DB, id uint) (*model.Dispute, error) {
	var d model.Dispute
	err := r.tx(tx).
		Preload("Contract.PartyA").
		Preload("Contract.PartyB").
		Preload("Contract.Requirement").
		Preload("Complainant").
		Preload("Admin").
		Preload("Supplements", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at ASC")
		}).
		Preload("Supplements.Submitter").
		First(&d, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find dispute by id: %w", err)
	}
	return &d, nil
}

// FindOpenByContract returns the in-process dispute of a contract, or nil.
func (r *DisputeRepository) FindOpenByContract(tx *gorm.DB, contractID uint) (*model.Dispute, error) {
	var d model.Dispute
	err := r.tx(tx).Where("contract_id = ? AND open_contract_id IS NOT NULL", contractID).First(&d).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("find open dispute: %w", err)
	}
	return &d, nil
}

// ListByParty returns disputes on contracts where the user is a party.
func (r *DisputeRepository) ListByParty(userID uint) ([]model.Dispute, error) {
	var list []model.Dispute
	err := r.db.
		Joins("JOIN contracts ON contracts.id = disputes.contract_id").
		Where("contracts.party_a_id = ? OR contracts.party_b_id = ?", userID, userID).
		Preload("Complainant").
		Preload("Admin").
		Order("disputes.created_at DESC").
		Find(&list).Error
	if err != nil {
		return nil, fmt.Errorf("list disputes by party: %w", err)
	}
	return list, nil
}

// ListAll returns every dispute (admin view).
func (r *DisputeRepository) ListAll() ([]model.Dispute, error) {
	var list []model.Dispute
	err := r.db.
		Preload("Complainant").
		Preload("Admin").
		Order("disputes.created_at DESC").
		Find(&list).Error
	if err != nil {
		return nil, fmt.Errorf("list all disputes: %w", err)
	}
	return list, nil
}

// TransitionStatus conditionally moves a dispute from expectStatus to nextStatus
// and applies the column updates in fields. It is the lost-update guard: under a
// concurrent ruling exactly one transaction sees RowsAffected == 1.
func (r *DisputeRepository) TransitionStatus(tx *gorm.DB, id uint, expectStatus, nextStatus string, fields map[string]any) (bool, error) {
	res := r.tx(tx).Model(&model.Dispute{}).
		Where("id = ? AND status = ?", id, expectStatus).
		Updates(mergeFields(fields, map[string]any{"status": nextStatus}))
	if res.Error != nil {
		return false, fmt.Errorf("transition dispute status: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

// FindOpenByContractIDs returns the open disputes of the given contracts in one
// query (used to enrich contract lists).
func (r *DisputeRepository) FindOpenByContractIDs(contractIDs []uint) ([]model.Dispute, error) {
	var list []model.Dispute
	if len(contractIDs) == 0 {
		return list, nil
	}
	err := r.db.Where("contract_id IN ? AND open_contract_id IS NOT NULL", contractIDs).
		Find(&list).Error
	if err != nil {
		return nil, fmt.Errorf("find open disputes by contracts: %w", err)
	}
	return list, nil
}

// FindLastRulingsByContractIDs returns, for each contract that has one, its most
// recent closed (ruled) dispute. Only status='ruled' rows qualify, so an
// in-process dispute can never be mistaken for a verdict.
func (r *DisputeRepository) FindLastRulingsByContractIDs(contractIDs []uint) ([]model.Dispute, error) {
	if len(contractIDs) == 0 {
		return nil, nil
	}
	// Pick the max (latest) ruled dispute id per contract, then load those rows.
	var latestIDs []uint
	sub := r.db.Model(&model.Dispute{}).
		Select("MAX(id)").
		Where("contract_id IN ? AND status = ?", contractIDs, constants.DisputeRuled).
		Group("contract_id")
	if err := r.db.Model(&model.Dispute{}).
		Where("id IN (?)", sub).
		Pluck("id", &latestIDs).Error; err != nil {
		return nil, fmt.Errorf("find last ruling ids: %w", err)
	}
	if len(latestIDs) == 0 {
		return nil, nil
	}
	var list []model.Dispute
	if err := r.db.Where("id IN ?", latestIDs).
		Preload("Admin").
		Preload("Complainant").
		Order("ruled_at DESC").
		Find(&list).Error; err != nil {
		return nil, fmt.Errorf("load last rulings: %w", err)
	}
	return list, nil
}

// FindLastRulingByContract returns the most recent ruled dispute of one contract,
// or nil if there is none.
func (r *DisputeRepository) FindLastRulingByContract(contractID uint) (*model.Dispute, error) {
	var d model.Dispute
	err := r.db.Where("contract_id = ? AND status = ?", contractID, constants.DisputeRuled).
		Order("ruled_at DESC, id DESC").
		First(&d).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("find last ruling: %w", err)
	}
	return &d, nil
}

// ListByContract returns all disputes (including closed) of one contract.
func (r *DisputeRepository) ListByContract(tx *gorm.DB, contractID uint) ([]model.Dispute, error) {
	var list []model.Dispute
	err := r.tx(tx).Where("contract_id = ?", contractID).
		Preload("Complainant").
		Preload("Admin").
		Preload("Supplements", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at ASC")
		}).
		Preload("Supplements.Submitter").
		Order("created_at DESC").
		Find(&list).Error
	if err != nil {
		return nil, fmt.Errorf("list disputes by contract: %w", err)
	}
	return list, nil
}

// Status returns the current status of a dispute.
func (r *DisputeRepository) Status(tx *gorm.DB, id uint) (string, error) {
	var d model.Dispute
	if err := r.tx(tx).Select("status").First(&d, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("load dispute status: %w", err)
	}
	return d.Status, nil
}

// CreateSupplement inserts one supplement round.
func (r *DisputeRepository) CreateSupplement(tx *gorm.DB, s *model.DisputeSupplement) error {
	if err := r.tx(tx).Create(s).Error; err != nil {
		return fmt.Errorf("create dispute supplement: %w", err)
	}
	return nil
}

// ListSupplements returns the supplement rounds of a dispute, oldest first.
func (r *DisputeRepository) ListSupplements(tx *gorm.DB, disputeID uint) ([]model.DisputeSupplement, error) {
	var list []model.DisputeSupplement
	err := r.tx(tx).Where("dispute_id = ?", disputeID).
		Preload("Submitter").
		Order("created_at ASC").
		Find(&list).Error
	if err != nil {
		return nil, fmt.Errorf("list dispute supplements: %w", err)
	}
	return list, nil
}

func mergeFields(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// isDuplicateKeyErr recognizes MySQL 1062 and SQLite UNIQUE violations.
func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDuplicate) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate entry") || strings.Contains(msg, "unique constraint failed")
}
