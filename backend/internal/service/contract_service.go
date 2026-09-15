package service

import (
	"fmt"
	"log/slog"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

// ContractService manages contracts.
type ContractService struct {
	contracts *repository.ContractRepository
	disputes  *repository.DisputeRepository
	logs      *OperationLogService
	logger    *slog.Logger
}

// NewContractService builds a ContractService.
func NewContractService(contracts *repository.ContractRepository, disputes *repository.DisputeRepository, logs *OperationLogService, logger *slog.Logger) *ContractService {
	return &ContractService{contracts: contracts, disputes: disputes, logs: logs, logger: logger}
}

// ListByParty returns contracts involving the caller, each annotated with its
// in-process dispute (if any) and latest ruling (if it has one).
func (s *ContractService) ListByParty(userID uint) ([]model.Contract, error) {
	list, err := s.contracts.ListByParty(userID)
	if err != nil {
		return nil, fmt.Errorf("list contracts: %w", err)
	}
	if err := s.attachDisputes(list); err != nil {
		return nil, err
	}
	return list, nil
}

// Get loads a contract and its in-process dispute / latest ruling (if any).
func (s *ContractService) Get(id uint) (*model.Contract, error) {
	c, err := s.contracts.FindByID(id)
	if err != nil {
		return nil, err
	}
	open, err := s.disputes.FindOpenByContract(nil, id)
	if err != nil {
		return nil, fmt.Errorf("load active dispute: %w", err)
	}
	last, err := s.disputes.FindLastRulingByContract(id)
	if err != nil {
		return nil, fmt.Errorf("load last ruling: %w", err)
	}
	c.ActiveDispute = open
	c.LastRuling = last
	return c, nil
}

// GetForViewer loads a contract but only embeds dispute data for an involved
// party or an administrator; outsiders still see the contract (existing
// behavior) without any dispute/ruling payload.
func (s *ContractService) GetForViewer(id, viewerID uint, viewerRole string) (*model.Contract, error) {
	c, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if viewerRole == constants.RoleAdmin || c.PartyAID == viewerID || c.PartyBID == viewerID {
		return c, nil
	}
	c.ActiveDispute = nil
	c.LastRuling = nil
	return c, nil
}

// attachDisputes annotates a contract slice with each contract's open dispute
// and its latest ruling in bulk.
func (s *ContractService) attachDisputes(list []model.Contract) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(list))
	for i := range list {
		ids = append(ids, list[i].ID)
	}

	open, err := s.disputes.FindOpenByContractIDs(ids)
	if err != nil {
		return fmt.Errorf("load active disputes: %w", err)
	}
	openByContract := make(map[uint]*model.Dispute, len(open))
	for i := range open {
		d := open[i]
		openByContract[d.ContractID] = &d
	}

	rulings, err := s.disputes.FindLastRulingsByContractIDs(ids)
	if err != nil {
		return fmt.Errorf("load last rulings: %w", err)
	}
	rulingByContract := make(map[uint]*model.Dispute, len(rulings))
	for i := range rulings {
		d := rulings[i]
		rulingByContract[d.ContractID] = &d
	}

	for i := range list {
		list[i].ActiveDispute = openByContract[list[i].ID]
		list[i].LastRuling = rulingByContract[list[i].ID]
	}
	return nil
}

// CreateFromBid builds a contract from an accepted bid.
func (s *ContractService) CreateFromBid(r *model.Requirement, bid *model.Bid, requesterID uint, requesterName string, paymentType string) (*model.Contract, error) {
	if paymentType == "" {
		paymentType = "one_time"
	}
	stages := []model.ContractStage{
		{Name: "项目启动", Amount: bid.Amount * 0.3, Status: "done", DueAt: "签约后3日内"},
		{Name: "中期交付", Amount: bid.Amount * 0.4, Status: "in_progress", DueAt: "工期过半"},
		{Name: "验收结项", Amount: bid.Amount * 0.3, Status: "pending", DueAt: "验收通过后"},
	}
	contract := &model.Contract{
		ContractNo:    fmt.Sprintf("CY-%d-%d", r.ID, bid.ID),
		TotalAmount:   bid.Amount,
		PaymentType:   paymentType,
		Stages:        stages,
		Status:        constants.ContractPendingSignature,
		RequirementID: r.ID,
		PartyAID:      requesterID,
		PartyBID:      bid.BidderID,
	}
	if err := s.contracts.Create(contract); err != nil {
		return nil, fmt.Errorf("create contract: %w", err)
	}
	s.logs.Record(requesterID, requesterName, "contract.create", "contract", contract.ID, fmt.Sprintf("生成合同 %s", contract.ContractNo))
	return contract, nil
}

// Sign confirms a contract by either party.
func (s *ContractService) Sign(id uint, userID uint, userName string) (*model.Contract, error) {
	c, err := s.contracts.FindByID(id)
	if err != nil {
		return nil, err
	}
	if c.PartyAID != userID && c.PartyBID != userID {
		return nil, constants.ErrForbidden
	}
	if c.Status != constants.ContractPendingSignature {
		return nil, constants.NewAppError(constants.CodeConflict, "合同当前不可签署")
	}
	c.Status = constants.ContractInProgress
	if err := s.contracts.Update(c); err != nil {
		return nil, fmt.Errorf("sign contract: %w", err)
	}
	s.logs.Record(userID, userName, "contract.sign", "contract", c.ID, "签署确认合同")
	return c, nil
}

// Complete confirms completion (requester side).
func (s *ContractService) Complete(id uint, userID uint, userName string) (*model.Contract, error) {
	c, err := s.contracts.FindByID(id)
	if err != nil {
		return nil, err
	}
	if c.PartyAID != userID {
		return nil, constants.ErrForbidden
	}
	if c.Status != constants.ContractInProgress && c.Status != constants.ContractPendingReview {
		return nil, constants.NewAppError(constants.CodeConflict, "合同当前不可完成确认")
	}
	c.Status = constants.ContractCompleted
	for i := range c.Stages {
		c.Stages[i].Status = "done"
	}
	if err := s.contracts.Update(c); err != nil {
		return nil, fmt.Errorf("complete contract: %w", err)
	}
	s.logs.Record(userID, userName, "contract.complete", "contract", c.ID, "确认合同完成")
	return c, nil
}
