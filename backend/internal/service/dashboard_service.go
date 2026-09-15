package service

import (
	"log/slog"

	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

// DashboardData is the role-aware workbench payload.
type DashboardData struct {
	MyRequirements []model.Requirement `json:"myRequirements"`
	MyBids         []model.Bid         `json:"myBids"`
	MyContracts    []model.Contract    `json:"myContracts"`
	Counts         map[string]int64    `json:"counts"`
}

// DashboardService aggregates workbench data.
type DashboardService struct {
	requirements *repository.RequirementRepository
	bids         *repository.BidRepository
	contracts    *repository.ContractRepository
	disputes     *repository.DisputeRepository
	logger       *slog.Logger
}

// NewDashboardService builds a DashboardService.
func NewDashboardService(requirements *repository.RequirementRepository, bids *repository.BidRepository, contracts *repository.ContractRepository, disputes *repository.DisputeRepository, logger *slog.Logger) *DashboardService {
	return &DashboardService{requirements: requirements, bids: bids, contracts: contracts, disputes: disputes, logger: logger}
}

// Get returns the workbench payload for a user.
func (s *DashboardService) Get(userID uint) (*DashboardData, error) {
	myRequirements, err := s.requirements.ListByPublisher(userID)
	if err != nil {
		return nil, err
	}
	myBids, err := s.bids.ListByBidder(userID)
	if err != nil {
		return nil, err
	}
	myContracts, err := s.contracts.ListByParty(userID)
	if err != nil {
		return nil, err
	}
	var openDisputes int64
	if len(myContracts) > 0 {
		ids := make([]uint, 0, len(myContracts))
		for i := range myContracts {
			ids = append(ids, myContracts[i].ID)
		}
		open, err := s.disputes.FindOpenByContractIDs(ids)
		if err != nil {
			return nil, err
		}
		byContract := make(map[uint]*model.Dispute, len(open))
		for i := range open {
			d := open[i]
			byContract[d.ContractID] = &d
		}
		rulings, err := s.disputes.FindLastRulingsByContractIDs(ids)
		if err != nil {
			return nil, err
		}
		lastByContract := make(map[uint]*model.Dispute, len(rulings))
		for i := range rulings {
			d := rulings[i]
			lastByContract[d.ContractID] = &d
		}
		for i := range myContracts {
			myContracts[i].ActiveDispute = byContract[myContracts[i].ID]
			myContracts[i].LastRuling = lastByContract[myContracts[i].ID]
			if myContracts[i].ActiveDispute != nil {
				openDisputes++
			}
		}
	}
	reqCount, err := s.requirements.CountByPublisher(userID)
	if err != nil {
		return nil, err
	}
	bidCount, err := s.bids.CountByBidder(userID)
	if err != nil {
		return nil, err
	}
	contractCount, err := s.contracts.CountByParty(userID)
	if err != nil {
		return nil, err
	}
	return &DashboardData{
		MyRequirements: myRequirements,
		MyBids:         myBids,
		MyContracts:    myContracts,
		Counts: map[string]int64{
			"requirements": reqCount,
			"bids":         bidCount,
			"contracts":    contractCount,
			"openDisputes": openDisputes,
		},
	}, nil
}
