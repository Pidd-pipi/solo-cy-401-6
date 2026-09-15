package service

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

var flowDBCounter uint64

func newFlowTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// A unique shared in-memory DSN per call keeps tests isolated (and safe
	// under -count=N) while retaining a single live connection.
	dsn := fmt.Sprintf("file:flow%d?mode=memory&cache=shared", atomic.AddUint64(&flowDBCounter, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.Requirement{}, &model.Bid{}, &model.Contract{},
		&model.Dispute{}, &model.DisputeSupplement{}, &model.OperationLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestAcceptBidCreatesContract(t *testing.T) {
	db := newFlowTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	userRepo := repository.NewUserRepository(db)
	reqRepo := repository.NewRequirementRepository(db)
	bidRepo := repository.NewBidRepository(db)
	contractRepo := repository.NewContractRepository(db)
	disputeRepo := repository.NewDisputeRepository(db)
	logRepo := repository.NewOperationLogRepository(db)

	requester := &model.User{Username: "req1", PasswordHash: "x", Name: "需求方", Role: constants.RoleRequester}
	freelancer := &model.User{Username: "free1", PasswordHash: "x", Name: "自由职业者", Role: constants.RoleFreelancer}
	if err := userRepo.Create(requester); err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(freelancer); err != nil {
		t.Fatal(err)
	}

	logSvc := NewOperationLogService(logRepo, logger)
	contractSvc := NewContractService(contractRepo, disputeRepo, logSvc, logger)
	reqSvc := NewRequirementService(reqRepo, bidRepo, logSvc, logger)
	bidSvc := NewBidService(bidRepo, reqRepo, logSvc, logger)

	requirement, err := reqSvc.Create(dto.CreateRequirementRequest{
		Title: "开发官网后台", Description: "需要一个功能完整的后台管理系统", MinBudget: 30000, MaxBudget: 60000, Skills: []string{"Go"},
	}, requester.ID, requester.Name, requester.Role)
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}

	bid, err := bidSvc.Create(dto.CreateBidRequest{
		RequirementID: requirement.ID, Amount: 45000, DurationDays: 30, Proposal: "我有丰富的 Go 开发经验，可按时交付。",
	}, freelancer.ID, freelancer.Name, freelancer.Role)
	if err != nil {
		t.Fatalf("create bid: %v", err)
	}

	contract, err := reqSvc.AcceptBid(requirement.ID, bid.ID, requester.ID, requester.Name, "installments", contractSvc)
	if err != nil {
		t.Fatalf("accept bid: %v", err)
	}
	if contract.Status != constants.ContractPendingSignature {
		t.Fatalf("contract status = %s, want pending_signature", contract.Status)
	}
	if contract.PartyAID != requester.ID || contract.PartyBID != freelancer.ID {
		t.Fatalf("unexpected parties: %d / %d", contract.PartyAID, contract.PartyBID)
	}
	if contract.TotalAmount != 45000 {
		t.Fatalf("contract amount = %v, want 45000", contract.TotalAmount)
	}

	// Requirement should move to in_progress.
	loaded, err := reqRepo.FindByID(requirement.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != constants.RequirementInProgress || loaded.WinnerID != freelancer.ID {
		t.Fatalf("requirement status=%s winner=%d", loaded.Status, loaded.WinnerID)
	}
}
