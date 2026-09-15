package service

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

var disputeDBCounter uint64

// disputeEnv holds the wired services and seeded users for dispute tests.
type disputeEnv struct {
	db         *gorm.DB
	svc        *DisputeService
	contractSv *ContractService
	contractID uint
	partyA     uint
	partyB     uint
	outsider   uint
	admin      uint
}

func newDisputeEnv(t *testing.T) *disputeEnv {
	t.Helper()
	// One connection per call keeps each in-memory DB isolated across tests and
	// serializes writes, so the concurrent-ruling test is deterministic.
	dsn := fmt.Sprintf("file:dispute%d?mode=memory&cache=shared", atomic.AddUint64(&disputeDBCounter, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(
		&model.User{}, &model.Requirement{}, &model.Bid{},
		&model.Contract{}, &model.Dispute{}, &model.DisputeSupplement{}, &model.OperationLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	userRepo := repository.NewUserRepository(db)
	contractRepo := repository.NewContractRepository(db)
	disputeRepo := repository.NewDisputeRepository(db)
	logRepo := repository.NewOperationLogRepository(db)
	logSvc := NewOperationLogService(logRepo, logger)

	users := []*model.User{
		{Username: "dp_party_a", PasswordHash: "x", Name: "争议甲方", Role: constants.RoleRequester},
		{Username: "dp_party_b", PasswordHash: "x", Name: "争议乙方", Role: constants.RoleFreelancer},
		{Username: "dp_outsider", PasswordHash: "x", Name: "无关用户", Role: constants.RoleFreelancer},
		{Username: "dp_admin", PasswordHash: "x", Name: "争议管理员", Role: constants.RoleAdmin},
	}
	for _, u := range users {
		if err := userRepo.Create(u); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	contract := &model.Contract{
		ContractNo:    fmt.Sprintf("DISP-%d", users[0].ID),
		TotalAmount:   50000,
		PaymentType:   "one_time",
		Status:        constants.ContractInProgress,
		RequirementID: 1,
		PartyAID:      users[0].ID,
		PartyBID:      users[1].ID,
	}
	if err := contractRepo.Create(contract); err != nil {
		t.Fatalf("create contract: %v", err)
	}

	return &disputeEnv{
		db:         db,
		svc:        NewDisputeService(db, contractRepo, disputeRepo, logSvc, logger),
		contractSv: NewContractService(contractRepo, disputeRepo, logSvc, logger),
		contractID: contract.ID,
		partyA:     users[0].ID,
		partyB:     users[1].ID,
		outsider:   users[2].ID,
		admin:      users[3].ID,
	}
}

func (e *disputeEnv) validRule() RulingInput {
	return RulingInput{
		Responsibility: "乙方未按约定时间交付，承担主要责任",
		RulingParty:    "party_b",
		RefundAmount:   20000,
		Opinion:        "依据双方沟通记录与交付物，裁决部分退款。",
	}
}

func appErrorCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *constants.AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	t.Fatalf("expected *AppError, got %v", err)
	return 0
}

// TestDisputeMainFlow exercises file -> request supplement -> supplement ->
// accept -> rule and asserts the ruling persists every required field.
func TestDisputeMainFlow(t *testing.T) {
	e := newDisputeEnv(t)

	d, err := e.svc.File(e.contractID, e.partyA, "争议甲方", constants.RoleRequester,
		"乙方延期交付且质量不达标", "要求退还部分款项", []string{"evidence1.png", "evidence2.pdf"})
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if d.Status != constants.DisputeSubmitted {
		t.Fatalf("status=%s want submitted", d.Status)
	}
	if len(d.Evidence) != 2 {
		t.Fatalf("evidence len=%d want 2", len(d.Evidence))
	}

	// Admin requests more material before accepting.
	d, err = e.svc.RequestSupplement(d.ID, e.admin, "争议管理员", constants.RoleAdmin, "请补充验收记录")
	if err != nil {
		t.Fatalf("request supplement: %v", err)
	}
	if d.Status != constants.DisputeAwaitingSupplement || d.SupplementNote != "请补充验收记录" {
		t.Fatalf("unexpected state after request supplement: %+v", d)
	}

	// Party submits the material -> back to accepted.
	d, err = e.svc.Supplement(d.ID, e.partyB, "争议乙方", "补充验收沟通截图", []string{"proof.png"})
	if err != nil {
		t.Fatalf("supplement: %v", err)
	}
	if d.Status != constants.DisputeAccepted {
		t.Fatalf("status=%s want accepted", d.Status)
	}
	if len(d.Supplements) != 1 || d.Supplements[0].Content != "补充验收沟通截图" {
		t.Fatalf("supplement round not persisted: %+v", d.Supplements)
	}

	// Rule.
	ruled, err := e.svc.Rule(d.ID, e.admin, "争议管理员", constants.RoleAdmin, e.validRule())
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	if ruled.Status != constants.DisputeRuled {
		t.Fatalf("status=%s want ruled", ruled.Status)
	}
	if ruled.Responsibility == "" || ruled.Opinion == "" || ruled.RulingParty != "party_b" {
		t.Fatalf("ruling fields missing: %+v", ruled)
	}
	if ruled.RefundAmount == nil || *ruled.RefundAmount != 20000 {
		t.Fatalf("refund amount not persisted: %+v", ruled.RefundAmount)
	}
	if ruled.RuledAt == nil {
		t.Fatal("ruledAt must be recorded")
	}
	if ruled.OpenContractID != nil {
		t.Fatal("open slot must be released after ruling")
	}
}

// TestDisputeAcceptThenRuleFlow covers the simpler file -> accept -> rule path.
func TestDisputeAcceptThenRuleFlow(t *testing.T) {
	e := newDisputeEnv(t)
	d, err := e.svc.File(e.contractID, e.partyB, "争议乙方", constants.RoleFreelancer,
		"甲方无理由拖欠尾款已超过三十天", "要求支付尾款", nil)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if d, err = e.svc.Accept(d.ID, e.admin, "争议管理员", constants.RoleAdmin); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if d.Status != constants.DisputeAccepted || d.AdminID == nil || *d.AdminID != e.admin {
		t.Fatalf("accept state wrong: %+v", d)
	}
	if _, err := e.svc.Rule(d.ID, e.admin, "争议管理员", constants.RoleAdmin, e.validRule()); err != nil {
		t.Fatalf("rule: %v", err)
	}
}

// TestDisputeRejections verifies each rejection path returns a distinct code.
func TestDisputeRejections(t *testing.T) {
	e := newDisputeEnv(t)

	// Non-party cannot file.
	_, err := e.svc.File(e.contractID, e.outsider, "无关用户", constants.RoleFreelancer,
		"我不是合同当事方但要发起争议", "无", nil)
	if code := appErrorCode(t, err); code != constants.CodeDisputeNotParty {
		t.Fatalf("outsider file code=%d want %d", code, constants.CodeDisputeNotParty)
	}

	d, err := e.svc.File(e.contractID, e.partyA, "争议甲方", constants.RoleRequester,
		"乙方延期交付且质量不达标", "要求退款", nil)
	if err != nil {
		t.Fatalf("file: %v", err)
	}

	// Duplicate filing while one is open.
	_, err = e.svc.File(e.contractID, e.partyB, "争议乙方", constants.RoleFreelancer,
		"我也要对同一合同提起争议", "反诉", nil)
	if code := appErrorCode(t, err); code != constants.CodeDisputeAlreadyOpen {
		t.Fatalf("duplicate file code=%d want %d", code, constants.CodeDisputeAlreadyOpen)
	}

	// Non-admin cannot accept / request supplement / rule.
	_, err = e.svc.Accept(d.ID, e.partyA, "争议甲方", constants.RoleRequester)
	if code := appErrorCode(t, err); code != constants.CodeDisputeNotAdmin {
		t.Fatalf("party accept code=%d want %d", code, constants.CodeDisputeNotAdmin)
	}
	_, err = e.svc.RequestSupplement(d.ID, e.partyA, "争议甲方", constants.RoleRequester, "note")
	if code := appErrorCode(t, err); code != constants.CodeDisputeNotAdmin {
		t.Fatalf("party request-supplement code=%d want %d", code, constants.CodeDisputeNotAdmin)
	}
	_, err = e.svc.Rule(d.ID, e.partyA, "争议甲方", constants.RoleRequester, e.validRule())
	if code := appErrorCode(t, err); code != constants.CodeDisputeNotAdmin {
		t.Fatalf("party rule code=%d want %d", code, constants.CodeDisputeNotAdmin)
	}

	// Illegal transitions while still submitted.
	if _, err := e.svc.Rule(d.ID, e.admin, "争议管理员", constants.RoleAdmin, e.validRule()); err == nil {
		t.Fatal("ruling before accepting must be rejected")
	} else if code := appErrorCode(t, err); code != constants.CodeDisputeIllegalTransition {
		t.Fatalf("rule-before-accept code=%d want %d", code, constants.CodeDisputeIllegalTransition)
	}

	// Supplement is only valid after an admin requested it.
	_, err = e.svc.Supplement(d.ID, e.partyA, "争议甲方", "主动补充材料", nil)
	if code := appErrorCode(t, err); code != constants.CodeDisputeIllegalTransition {
		t.Fatalf("supplement-before-request code=%d want %d", code, constants.CodeDisputeIllegalTransition)
	}

	// Non-party cannot supplement even in the right state.
	if _, err := e.svc.RequestSupplement(d.ID, e.admin, "争议管理员", constants.RoleAdmin, "请补材料"); err != nil {
		t.Fatalf("request supplement: %v", err)
	}
	_, err = e.svc.Supplement(d.ID, e.outsider, "无关用户", "我也要补材料", nil)
	if code := appErrorCode(t, err); code != constants.CodeDisputeNotParty {
		t.Fatalf("outsider supplement code=%d want %d", code, constants.CodeDisputeNotParty)
	}

	// Non-party / non-admin cannot read the dispute.
	if _, err := e.svc.Get(d.ID, e.outsider, constants.RoleFreelancer); err == nil {
		t.Fatal("outsider must not read the dispute")
	} else if code := appErrorCode(t, err); code != constants.CodeDisputeNotParty {
		t.Fatalf("outsider get code=%d want %d", code, constants.CodeDisputeNotParty)
	}

	// Drive to accepted then ruled.
	if _, err := e.svc.Supplement(d.ID, e.partyA, "争议甲方", "材料补齐", nil); err != nil {
		t.Fatalf("supplement: %v", err)
	}
	ruled, err := e.svc.Rule(d.ID, e.admin, "争议管理员", constants.RoleAdmin, e.validRule())
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	// Closed dispute is immutable: accept / supplement-request / supplement / rule all rejected.
	cases := map[string]func() error{
		"accept": func() error {
			_, e2 := e.svc.Accept(ruled.ID, e.admin, "争议管理员", constants.RoleAdmin)
			return e2
		},
		"request": func() error {
			_, e2 := e.svc.RequestSupplement(ruled.ID, e.admin, "争议管理员", constants.RoleAdmin, "x")
			return e2
		},
		"supplement": func() error {
			_, e2 := e.svc.Supplement(ruled.ID, e.partyA, "争议甲方", "再补材料", nil)
			return e2
		},
		"rule": func() error {
			_, e2 := e.svc.Rule(ruled.ID, e.admin, "争议管理员", constants.RoleAdmin, e.validRule())
			return e2
		},
	}
	for name, fn := range cases {
		if code := appErrorCode(t, fn()); code != constants.CodeDisputeClosed {
			t.Fatalf("closed %s code=%d want %d", name, code, constants.CodeDisputeClosed)
		}
	}

	// A new dispute may be filed after the previous one closed (one open slot rule).
	d2, err := e.svc.File(e.contractID, e.partyA, "争议甲方", constants.RoleRequester,
		"就尾款执行问题再次发起争议", "继续追讨", nil)
	if err != nil {
		t.Fatalf("re-file after close: %v", err)
	}
	if d2.ID == ruled.ID {
		t.Fatal("re-file must create a new dispute record")
	}
}

// TestDisputeNotFound maps unknown ids to the repository not-found sentinel.
func TestDisputeNotFound(t *testing.T) {
	e := newDisputeEnv(t)
	if _, err := e.svc.Accept(999999, e.admin, "争议管理员", constants.RoleAdmin); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("accept missing err=%v want ErrNotFound", err)
	}
	if _, err := e.svc.File(999999, e.partyA, "争议甲方", constants.RoleRequester, "x", "y", nil); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("file on missing contract err=%v want ErrNotFound", err)
	}
}

// TestDisputeConcurrentRule fires two rulings on the same accepted dispute
// concurrently. Exactly one may succeed. Depending on isolation/timing the loser
// either re-reads the committed "ruled" row (CodeDisputeClosed) or loses the
// compare-and-swap after reading the same snapshot (CodeDisputeConcurrent); both
// are explicit, distinguishable rejections — never a second applied verdict.
func TestDisputeConcurrentRule(t *testing.T) {
	e := newDisputeEnv(t)
	d, err := e.svc.File(e.contractID, e.partyA, "争议甲方", constants.RoleRequester,
		"乙方延期交付且质量不达标", "要求退款", nil)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if _, err := e.svc.Accept(d.ID, e.admin, "争议管理员", constants.RoleAdmin); err != nil {
		t.Fatalf("accept: %v", err)
	}

	const n = 2
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			in := e.validRule()
			in.Opinion = fmt.Sprintf("并发裁决-%d", i)
			_, results[i] = e.svc.Rule(d.ID, e.admin, "争议管理员", constants.RoleAdmin, in)
		}()
	}
	close(start)
	wg.Wait()

	success, rejected := 0, 0
	for i, rerr := range results {
		switch {
		case rerr == nil:
			success++
		case rerr != nil:
			code := appErrorCode(t, rerr)
			if code != constants.CodeDisputeClosed && code != constants.CodeDisputeConcurrent {
				t.Fatalf("ruling %d rejected with unexpected code %d (%v)", i, code, rerr)
			}
			rejected++
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("ruling results success=%d rejected=%d, want 1/1 (one verdict applied)", success, rejected)
	}

	// Final state is ruled exactly once; re-reading shows a single immutable verdict.
	final, err := e.disputesRepoOrFail(t, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != constants.DisputeRuled {
		t.Fatalf("final status=%s want ruled", final.Status)
	}
}

// TestDisputeConcurrentRuleCAS proves at the data layer that two adjudications
// racing on the same expected status can apply at most once: the conditional
// UPDATE (WHERE status = expected) affects one row for the winner and zero for
// the loser — the lost-update guard behind CodeDisputeConcurrent.
func TestDisputeConcurrentRuleCAS(t *testing.T) {
	e := newDisputeEnv(t)
	repo := repository.NewDisputeRepository(e.db)
	d, err := e.svc.File(e.contractID, e.partyA, "争议甲方", constants.RoleRequester,
		"乙方延期交付且质量不达标", "要求退款", nil)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if _, err := e.svc.Accept(d.ID, e.admin, "争议管理员", constants.RoleAdmin); err != nil {
		t.Fatalf("accept: %v", err)
	}

	fields := map[string]any{"responsibility": "各半", "ruling_party": "shared", "refund_amount": 0.0, "opinion": "x", "open_contract_id": nil}
	first, err := repo.TransitionStatus(nil, d.ID, constants.DisputeAccepted, constants.DisputeRuled, fields)
	if err != nil {
		t.Fatalf("first transition: %v", err)
	}
	if !first {
		t.Fatal("first conditional ruling must apply")
	}
	// The loser still believes status is accepted: its conditional update is a no-op.
	second, err := repo.TransitionStatus(nil, d.ID, constants.DisputeAccepted, constants.DisputeRuled, fields)
	if err != nil {
		t.Fatalf("second transition: %v", err)
	}
	if second {
		t.Fatal("second conditional ruling must NOT apply — only one verdict may land")
	}
}

// TestContractExposesActiveDispute asserts contract detail/list carry the open
// dispute while processed, drop it after closing, and expose the latest ruling
// (liability + refund) afterwards so it never "disappears" from the workbench.
func TestContractExposesActiveDispute(t *testing.T) {
	e := newDisputeEnv(t)
	d, err := e.svc.File(e.contractID, e.partyA, "争议甲方", constants.RoleRequester,
		"乙方延期交付且质量不达标", "要求退款", nil)
	if err != nil {
		t.Fatalf("file: %v", err)
	}

	c, err := e.contractSv.Get(e.contractID)
	if err != nil {
		t.Fatal(err)
	}
	if c.ActiveDispute == nil || c.ActiveDispute.ID != d.ID {
		t.Fatalf("contract detail must expose active dispute, got %+v", c.ActiveDispute)
	}
	if c.LastRuling != nil {
		t.Fatalf("no ruling expected yet, got %+v", c.LastRuling)
	}

	// Workbench list must show the open dispute too.
	list, err := e.contractSv.ListByParty(e.partyA)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ActiveDispute == nil || list[0].ActiveDispute.ID != d.ID || list[0].LastRuling != nil {
		t.Fatalf("list before ruling: active=%+v last=%+v", list[0].ActiveDispute, list[0].LastRuling)
	}

	if _, err := e.svc.Accept(d.ID, e.admin, "争议管理员", constants.RoleAdmin); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, err := e.svc.Rule(d.ID, e.admin, "争议管理员", constants.RoleAdmin, e.validRule()); err != nil {
		t.Fatalf("rule: %v", err)
	}
	c, err = e.contractSv.Get(e.contractID)
	if err != nil {
		t.Fatal(err)
	}
	if c.ActiveDispute != nil {
		t.Fatalf("closed dispute must not be reported as active, got %+v", c.ActiveDispute)
	}
	if c.LastRuling == nil {
		t.Fatal("contract detail must keep showing the latest ruling after closing")
	}
	if c.LastRuling.RulingParty != "party_b" {
		t.Fatalf("last ruling party=%q want party_b", c.LastRuling.RulingParty)
	}
	if c.LastRuling.RefundAmount == nil || *c.LastRuling.RefundAmount != 20000 {
		t.Fatalf("last ruling refund=%v want 20000", c.LastRuling.RefundAmount)
	}
	if c.LastRuling.Responsibility == "" || c.LastRuling.Opinion == "" || c.LastRuling.RuledAt == nil {
		t.Fatalf("last ruling missing required fields: %+v", c.LastRuling)
	}

	// Workbench list must carry the verdict, not lose it.
	list, err = e.contractSv.ListByParty(e.partyA)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ActiveDispute != nil {
		t.Fatalf("list after ruling: activeDispute must be nil")
	}
	if list[0].LastRuling == nil || list[0].LastRuling.ID != d.ID {
		t.Fatalf("list after ruling must expose last ruling #%d, got %+v", d.ID, list[0].LastRuling)
	}
}

// TestLastRulingLatestAndNeverOpen covers two guarantees:
//  1. a new open dispute keeps showing as active WITHOUT replacing the prior verdict;
//  2. after re-filing and a second ruling, LastRuling is the newest verdict.
func TestLastRulingLatestAndNeverOpen(t *testing.T) {
	e := newDisputeEnv(t)
	rule := func(disputeID uint, refund float64, who string) {
		t.Helper()
		in := e.validRule()
		in.RefundAmount = refund
		in.RulingParty = who
		if _, err := e.svc.Rule(disputeID, e.admin, "争议管理员", constants.RoleAdmin, in); err != nil {
			t.Fatalf("rule #%d: %v", disputeID, err)
		}
	}

	// First dispute -> accept -> rule.
	d1, err := e.svc.File(e.contractID, e.partyA, "争议甲方", constants.RoleRequester,
		"乙方第一次延期交付", "第一次退款", nil)
	if err != nil {
		t.Fatalf("file 1: %v", err)
	}
	if _, err := e.svc.Accept(d1.ID, e.admin, "争议管理员", constants.RoleAdmin); err != nil {
		t.Fatalf("accept 1: %v", err)
	}
	rule(d1.ID, 20000, "party_b")

	// Re-file a second dispute. Now: active=d2, lastRuling still d1.
	d2, err := e.svc.File(e.contractID, e.partyB, "争议乙方", constants.RoleFreelancer,
		"甲方对整改后尾款仍有异议", "重新裁定", nil)
	if err != nil {
		t.Fatalf("file 2: %v", err)
	}
	c, err := e.contractSv.Get(e.contractID)
	if err != nil {
		t.Fatal(err)
	}
	if c.ActiveDispute == nil || c.ActiveDispute.ID != d2.ID {
		t.Fatalf("second dispute must be active, got %+v", c.ActiveDispute)
	}
	if c.LastRuling == nil || c.LastRuling.ID != d1.ID {
		t.Fatalf("first verdict must remain while new dispute is open, got %+v", c.LastRuling)
	}

	// Rule the second dispute -> lastRuling must switch to d2.
	if _, err := e.svc.Accept(d2.ID, e.admin, "争议管理员", constants.RoleAdmin); err != nil {
		t.Fatalf("accept 2: %v", err)
	}
	rule(d2.ID, 5000, "shared")
	c, err = e.contractSv.Get(e.contractID)
	if err != nil {
		t.Fatal(err)
	}
	if c.ActiveDispute != nil {
		t.Fatalf("no open dispute after second ruling, got %+v", c.ActiveDispute)
	}
	if c.LastRuling == nil || c.LastRuling.ID != d2.ID {
		t.Fatalf("last ruling should be newest #%d, got %+v", d2.ID, c.LastRuling)
	}
	if c.LastRuling.RefundAmount == nil || *c.LastRuling.RefundAmount != 5000 || c.LastRuling.RulingParty != "shared" {
		t.Fatalf("last ruling fields wrong: %+v", c.LastRuling)
	}
}

func (e *disputeEnv) disputesRepoOrFail(t *testing.T, id uint) (*model.Dispute, error) {
	t.Helper()
	repo := repository.NewDisputeRepository(e.db)
	return repo.FindByID(nil, id)
}
