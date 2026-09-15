package service

import (
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

// DisputeService implements the contract-dispute workflow.
//
// Every mutating operation runs in a transaction, validates the state machine
// against the row it read, and ends with a conditional UPDATE
// (WHERE status = expected). A sequential illegal jump is reported as
// CodeDisputeIllegalTransition/CodeDisputeClosed; a genuine lost update (the
// row changed between read and write) is reported as CodeDisputeConcurrent, so
// concurrent adjudication can never be applied twice. The unique index on
// open_contract_id additionally guarantees at most one open dispute per contract.
type DisputeService struct {
	db        *gorm.DB
	contracts *repository.ContractRepository
	disputes  *repository.DisputeRepository
	logs      *OperationLogService
	logger    *slog.Logger
}

// NewDisputeService builds a DisputeService.
func NewDisputeService(db *gorm.DB, contracts *repository.ContractRepository, disputes *repository.DisputeRepository, logs *OperationLogService, logger *slog.Logger) *DisputeService {
	return &DisputeService{db: db, contracts: contracts, disputes: disputes, logs: logs, logger: logger}
}

// File is submitted by either contract party.
func (s *DisputeService) File(contractID, userID uint, userName, role, reason, claim string, evidence []string) (*model.Dispute, error) {
	created := &model.Dispute{}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		contract, err := s.contracts.FindByIDTx(tx, contractID)
		if err != nil {
			return err
		}
		if contract.PartyAID != userID && contract.PartyBID != userID {
			return constants.NewAppError(constants.CodeDisputeNotParty, "只有合同当事方可以提交争议")
		}
		// Cheap pre-check; the unique index remains the hard guarantee.
		open, err := s.disputes.FindOpenByContract(tx, contractID)
		if err != nil {
			return fmt.Errorf("check open dispute: %w", err)
		}
		if open != nil {
			return constants.NewAppError(constants.CodeDisputeAlreadyOpen, "该合同已有一条处理中的争议，不可重复提交")
		}

		d := &model.Dispute{
			ContractID:     contractID,
			OpenContractID: &contractID,
			ComplainantID:  userID,
			Reason:         reason,
			Claim:          claim,
			Evidence:       nonNilStrings(evidence),
			Status:         constants.DisputeSubmitted,
		}
		if err := s.disputes.Create(tx, d); err != nil {
			if err == repository.ErrDuplicate {
				return constants.NewAppError(constants.CodeDisputeAlreadyOpen, "该合同已有一条处理中的争议，不可重复提交")
			}
			return fmt.Errorf("persist dispute: %w", err)
		}
		created = d
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.logs.Record(userID, userName, "dispute.file", "contract", contractID, fmt.Sprintf("提交争议 #%d，诉求：%s", created.ID, claim))
	return created, nil
}

// Accept is an administrator accepting a filed dispute for processing.
func (s *DisputeService) Accept(disputeID, adminID uint, adminName, role string) (*model.Dispute, error) {
	if err := requireAdmin(role); err != nil {
		return nil, err
	}
	d, err := s.cas(disputeID, func(_ *gorm.DB, cur *model.Dispute) (string, map[string]any, error) {
		switch cur.Status {
		case constants.DisputeSubmitted:
			return constants.DisputeAccepted, map[string]any{"admin_id": adminID}, nil
		case constants.DisputeRuled:
			return "", nil, closedErr("争议已裁决关闭，不可再受理")
		default:
			return "", nil, illegalTransition(cur.Status)
		}
	})
	if err != nil {
		return nil, err
	}
	s.logs.Record(adminID, adminName, "dispute.accept", "dispute", disputeID, "受理争议")
	return d, nil
}

// RequestSupplement is an administrator asking the parties for more material.
// Legal from submitted or accepted.
func (s *DisputeService) RequestSupplement(disputeID, adminID uint, adminName, role, note string) (*model.Dispute, error) {
	if err := requireAdmin(role); err != nil {
		return nil, err
	}
	d, err := s.cas(disputeID, func(_ *gorm.DB, cur *model.Dispute) (string, map[string]any, error) {
		switch cur.Status {
		case constants.DisputeSubmitted, constants.DisputeAccepted:
			return constants.DisputeAwaitingSupplement, map[string]any{"admin_id": adminID, "supplement_note": note}, nil
		case constants.DisputeRuled:
			return "", nil, closedErr("争议已关闭，不可再要求补充材料")
		default:
			return "", nil, illegalTransition(cur.Status)
		}
	})
	if err != nil {
		return nil, err
	}
	s.logs.Record(adminID, adminName, "dispute.request_supplement", "dispute", disputeID, "要求补充材料："+note)
	return d, nil
}

// Supplement is a contract party responding with additional material.
func (s *DisputeService) Supplement(disputeID, userID uint, userName, content string, attachments []string) (*model.Dispute, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		d, err := s.disputes.FindByID(tx, disputeID)
		if err != nil {
			return err
		}
		switch d.Status {
		case constants.DisputeRuled:
			return closedErr("争议已关闭，不可再补充材料")
		case constants.DisputeAwaitingSupplement:
			// allowed
		default:
			return illegalTransition(d.Status)
		}
		if err := ensureContractParty(tx, s.contracts, d.ContractID, userID); err != nil {
			return err
		}
		round := &model.DisputeSupplement{
			DisputeID:   disputeID,
			SubmitterID: userID,
			Content:     content,
			Attachments: nonNilStrings(attachments),
		}
		if err := s.disputes.CreateSupplement(tx, round); err != nil {
			return err
		}
		ok, err := s.disputes.TransitionStatus(tx, disputeID, constants.DisputeAwaitingSupplement, constants.DisputeAccepted, nil)
		if err != nil {
			return err
		}
		if !ok {
			return concurrentErr()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.logs.Record(userID, userName, "dispute.supplement", "dispute", disputeID, "补充争议材料")
	return s.disputes.FindByID(nil, disputeID)
}

// RulingInput carries the four mandatory fields of a ruling.
type RulingInput struct {
	Responsibility string
	RulingParty    string
	RefundAmount   float64
	Opinion        string
}

// Rule is the final administrator adjudication. Legal only from accepted.
func (s *DisputeService) Rule(disputeID, adminID uint, adminName, role string, in RulingInput) (*model.Dispute, error) {
	if err := requireAdmin(role); err != nil {
		return nil, err
	}
	now := time.Now()
	d, err := s.cas(disputeID, func(_ *gorm.DB, cur *model.Dispute) (string, map[string]any, error) {
		switch cur.Status {
		case constants.DisputeAccepted:
			return constants.DisputeRuled, map[string]any{
				"admin_id":         adminID,
				"responsibility":   in.Responsibility,
				"ruling_party":     in.RulingParty,
				"refund_amount":    in.RefundAmount,
				"opinion":          in.Opinion,
				"ruled_at":         now,
				"open_contract_id": nil, // release the per-contract open slot
			}, nil
		case constants.DisputeRuled:
			return "", nil, closedErr("争议已裁决关闭，不可重复裁决或修改")
		default:
			return "", nil, illegalTransition(cur.Status)
		}
	})
	if err != nil {
		return nil, err
	}
	s.logs.Record(adminID, adminName, "dispute.rule", "dispute", disputeID,
		fmt.Sprintf("裁决争议，责任：%s，应退金额：%.2f", in.Responsibility, in.RefundAmount))
	return d, nil
}

// Get returns one dispute; non-parties and non-admins are rejected.
func (s *DisputeService) Get(disputeID, userID uint, role string) (*model.Dispute, error) {
	d, err := s.disputes.FindByID(nil, disputeID)
	if err != nil {
		return nil, err
	}
	if role != constants.RoleAdmin && !d.InvolvesUser(userID) {
		return nil, constants.NewAppError(constants.CodeDisputeNotParty, "无权查看该争议")
	}
	return d, nil
}

// ListByContract returns the dispute history of one contract for an involved
// party or an administrator.
func (s *DisputeService) ListByContract(contractID, userID uint, role string) ([]model.Dispute, error) {
	if role != constants.RoleAdmin {
		contract, err := s.contracts.FindByID(contractID)
		if err != nil {
			return nil, err
		}
		if contract.PartyAID != userID && contract.PartyBID != userID {
			return nil, constants.NewAppError(constants.CodeDisputeNotParty, "无权查看该合同的争议")
		}
	}
	return s.disputes.ListByContract(nil, contractID)
}

// ListByParty returns disputes on the caller's contracts.
func (s *DisputeService) ListByParty(userID uint) ([]model.Dispute, error) {
	return s.disputes.ListByParty(userID)
}

// ListAll returns every dispute (admin only).
func (s *DisputeService) ListAll(role string) ([]model.Dispute, error) {
	if err := requireAdmin(role); err != nil {
		return nil, err
	}
	return s.disputes.ListAll()
}

// cas runs a status-machine transition inside a transaction. fn validates the
// current row and returns the next status and column updates. The conditional
// UPDATE is the lost-update guard.
func (s *DisputeService) cas(disputeID uint, fn func(tx *gorm.DB, current *model.Dispute) (next string, fields map[string]any, err error)) (*model.Dispute, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		cur, err := s.disputes.FindByID(tx, disputeID)
		if err != nil {
			return err
		}
		next, fields, err := fn(tx, cur)
		if err != nil {
			return err
		}
		ok, err := s.disputes.TransitionStatus(tx, disputeID, cur.Status, next, fields)
		if err != nil {
			return err
		}
		if !ok {
			return concurrentErr()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.disputes.FindByID(nil, disputeID)
}

func illegalTransition(current string) error {
	return constants.NewAppError(constants.CodeDisputeIllegalTransition, fmt.Sprintf("当前状态（%s）不允许该操作", current))
}

func closedErr(msg string) error {
	return constants.NewAppError(constants.CodeDisputeClosed, msg)
}

func concurrentErr() error {
	return constants.NewAppError(constants.CodeDisputeConcurrent, "争议状态已被并发操作改变，请刷新后重试")
}

func requireAdmin(role string) error {
	if role != constants.RoleAdmin {
		return constants.NewAppError(constants.CodeDisputeNotAdmin, "仅平台管理员可以执行该操作")
	}
	return nil
}

// ensureContractParty verifies userID is a party of the contract inside a tx.
func ensureContractParty(tx *gorm.DB, contracts *repository.ContractRepository, contractID, userID uint) error {
	contract, err := contracts.FindByIDTx(tx, contractID)
	if err != nil {
		return err
	}
	if contract.PartyAID != userID && contract.PartyBID != userID {
		return constants.NewAppError(constants.CodeDisputeNotParty, "只有合同当事方可以补充争议材料")
	}
	return nil
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
