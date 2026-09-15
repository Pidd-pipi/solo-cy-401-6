package repository

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/model"
)

var repoDBCounter uint64

func newDisputeRepoDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:drepo%d?mode=memory&cache=shared", atomic.AddUint64(&repoDBCounter, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.Contract{}, &model.Dispute{}, &model.DisputeSupplement{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestOpenDisputeUniqueIndex verifies the hard guarantee: two open disputes on
// the same contract cannot coexist, even if the service pre-check were bypassed.
func TestOpenDisputeUniqueIndex(t *testing.T) {
	db := newDisputeRepoDB(t)
	repo := NewDisputeRepository(db)

	contractID := uint(7)
	first := &model.Dispute{ContractID: contractID, OpenContractID: &contractID, ComplainantID: 1,
		Reason: "第一次争议的原因说明", Claim: "诉求一", Status: constants.DisputeSubmitted}
	if err := repo.Create(nil, first); err != nil {
		t.Fatalf("create first: %v", err)
	}

	second := &model.Dispute{ContractID: contractID, OpenContractID: &contractID, ComplainantID: 2,
		Reason: "并发提交的第二条争议说明", Claim: "诉求二", Status: constants.DisputeSubmitted}
	err := repo.Create(nil, second)
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second open dispute err=%v want ErrDuplicate", err)
	}

	// Different contract is fine.
	other := uint(8)
	third := &model.Dispute{ContractID: other, OpenContractID: &other, ComplainantID: 1,
		Reason: "另一份合同的争议说明", Claim: "诉求三", Status: constants.DisputeSubmitted}
	if err := repo.Create(nil, third); err != nil {
		t.Fatalf("create on other contract: %v", err)
	}
}

// TestLastRulingOnlyRuled verifies that FindLastRulings ignores every non-ruled
// status (an in-process dispute must never be displayed as a verdict) and that
// the newest ruled row wins after a re-file.
func TestLastRulingOnlyRuled(t *testing.T) {
	db := newDisputeRepoDB(t)
	repo := NewDisputeRepository(db)
	contractID := uint(21)

	// One closed ruling and one brand-new open dispute on the same contract.
	r1 := &model.Dispute{ContractID: contractID, OpenContractID: nil, ComplainantID: 1,
		Reason: "第一次争议的原因说明", Claim: "诉求一", Status: constants.DisputeRuled,
		RulingParty: "party_b", RefundAmount: ptrFloat(20000), Responsibility: "乙方主责", Opinion: "意见一"}
	if err := repo.Create(nil, r1); err != nil {
		t.Fatalf("create ruled 1: %v", err)
	}
	open := &model.Dispute{ContractID: contractID, OpenContractID: &contractID, ComplainantID: 2,
		Reason: "再次发起争议的原因说明", Claim: "诉求二", Status: constants.DisputeSubmitted}
	if err := repo.Create(nil, open); err != nil {
		t.Fatalf("create open: %v", err)
	}

	// While the new dispute is open, only the older ruling is a verdict.
	got, err := repo.FindLastRulingsByContractIDs([]uint{contractID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != r1.ID {
		t.Fatalf("last ruling must be the closed row only, got %+v", got)
	}
	single, err := repo.FindLastRulingByContract(contractID)
	if err != nil || single == nil || single.ID != r1.ID {
		t.Fatalf("single last ruling wrong: %+v err=%v", single, err)
	}

	// Close the open dispute with a newer ruling -> newest verdict wins.
	if _, err := repo.TransitionStatus(nil, open.ID, constants.DisputeSubmitted, constants.DisputeAccepted, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.TransitionStatus(nil, open.ID, constants.DisputeAccepted, constants.DisputeRuled,
		map[string]any{"open_contract_id": nil, "ruling_party": "shared", "refund_amount": 5000.0,
			"responsibility": "各半", "opinion": "意见二"}); err != nil {
		t.Fatal(err)
	}
	got, err = repo.FindLastRulingsByContractIDs([]uint{contractID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != open.ID {
		t.Fatalf("last ruling should switch to newest #%d, got %+v", open.ID, got)
	}
	if got[0].RulingParty != "shared" {
		t.Fatalf("newest ruling party=%q want shared", got[0].RulingParty)
	}
}

func ptrFloat(v float64) *float64 { return &v }

func TestClosedDisputeReleasesSlot(t *testing.T) {
	db := newDisputeRepoDB(t)
	repo := NewDisputeRepository(db)
	contractID := uint(9)

	d := &model.Dispute{ContractID: contractID, OpenContractID: &contractID, ComplainantID: 1,
		Reason: "争议原因说明", Claim: "诉求", Status: constants.DisputeAccepted}
	if err := repo.Create(nil, d); err != nil {
		t.Fatalf("create: %v", err)
	}
	ok, err := repo.TransitionStatus(nil, d.ID, constants.DisputeAccepted, constants.DisputeRuled,
		map[string]any{"open_contract_id": nil, "responsibility": "对半"})
	if err != nil || !ok {
		t.Fatalf("close transition ok=%v err=%v", ok, err)
	}

	open, err := repo.FindOpenByContract(nil, contractID)
	if err != nil {
		t.Fatal(err)
	}
	if open != nil {
		t.Fatal("no open dispute expected after ruling")
	}

	next := &model.Dispute{ContractID: contractID, OpenContractID: &contractID, ComplainantID: 2,
		Reason: "关闭后再次发起争议说明", Claim: "新诉求", Status: constants.DisputeSubmitted}
	if err := repo.Create(nil, next); err != nil {
		t.Fatalf("re-file after close: %v", err)
	}
}
