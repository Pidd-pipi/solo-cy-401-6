package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/config"
	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/handler"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
	"github.com/gigmatch/gigmatch/internal/service"
	"github.com/gigmatch/gigmatch/internal/util"
)

var httpDBCounter uint64

type disputeHTTPEnv struct {
	engine     http.Handler
	cfg        *config.Config
	partyA     *model.User
	partyB     *model.User
	outsider   *model.User
	admin      *model.User
	contractID uint
}

func newDisputeHTTPEnv(t *testing.T) *disputeHTTPEnv {
	t.Helper()
	dsn := fmt.Sprintf("file:http%d?mode=memory&cache=shared", atomic.AddUint64(&httpDBCounter, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&model.User{}, &model.Requirement{}, &model.Bid{}, &model.Contract{},
		&model.Dispute{}, &model.DisputeSupplement{}, &model.OperationLog{},
	); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
	userRepo := repository.NewUserRepository(db)
	reqRepo := repository.NewRequirementRepository(db)
	bidRepo := repository.NewBidRepository(db)
	contractRepo := repository.NewContractRepository(db)
	disputeRepo := repository.NewDisputeRepository(db)
	logRepo := repository.NewOperationLogRepository(db)

	logSvc := service.NewOperationLogService(logRepo, logger)
	cfg := &config.Config{Env: "test", ServerPort: 8080, JWTSecret: "integration-test-secret-at-least-32-chars",
		CORSAllowedOrigins: "http://localhost:28030", AuthRateLimit: 100000, APIRateLimit: 100000}
	authSvc := service.NewAuthService(cfg, userRepo, logSvc, logger)
	_ = authSvc
	userSvc := service.NewUserService(userRepo, logSvc, logger)
	contractSvc := service.NewContractService(contractRepo, disputeRepo, logSvc, logger)
	disputeSvc := service.NewDisputeService(db, contractRepo, disputeRepo, logSvc, logger)
	bidSvc := service.NewBidService(bidRepo, reqRepo, logSvc, logger)
	reqSvc := service.NewRequirementService(reqRepo, bidRepo, logSvc, logger)
	dashSvc := service.NewDashboardService(reqRepo, bidRepo, contractRepo, disputeRepo, logger)

	h := &Handlers{
		Auth:         handler.NewAuthHandler(authSvc, logger),
		User:         handler.NewUserHandler(userSvc, logger),
		Requirement:  handler.NewRequirementHandler(reqSvc, contractSvc, logger),
		Bid:          handler.NewBidHandler(bidSvc, logger),
		Contract:     handler.NewContractHandler(contractSvc, logger),
		Dispute:      handler.NewDisputeHandler(disputeSvc, logger),
		Dashboard:    handler.NewDashboardHandler(dashSvc, logger),
		OperationLog: handler.NewOperationLogHandler(logSvc, logger),
	}
	engine := New(cfg, logger, h, userRepo, logSvc, db)

	users := []*model.User{
		{Username: "h_party_a", PasswordHash: "x", Name: "甲方", Role: constants.RoleRequester},
		{Username: "h_party_b", PasswordHash: "x", Name: "乙方", Role: constants.RoleFreelancer},
		{Username: "h_outsider", PasswordHash: "x", Name: "无关方", Role: constants.RoleFreelancer},
		{Username: "h_admin", PasswordHash: "x", Name: "管理员", Role: constants.RoleAdmin},
	}
	for _, u := range users {
		if err := userRepo.Create(u); err != nil {
			t.Fatal(err)
		}
	}
	contract := &model.Contract{ContractNo: "HTTP-1", TotalAmount: 50000, PaymentType: "one_time",
		Status: constants.ContractInProgress, PartyAID: users[0].ID, PartyBID: users[1].ID}
	if err := contractRepo.Create(contract); err != nil {
		t.Fatal(err)
	}
	return &disputeHTTPEnv{
		engine: engine, cfg: cfg,
		partyA: users[0], partyB: users[1], outsider: users[2], admin: users[3],
		contractID: contract.ID,
	}
}

func (e *disputeHTTPEnv) token(t *testing.T, u *model.User) string {
	t.Helper()
	tok, err := util.GenerateToken(e.cfg.JWTSecret, u.ID, u.Role, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type apiBody struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *disputeHTTPEnv) do(t *testing.T, method, path, token string, payload any) (int, apiBody) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var got apiBody
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	return rec.Code, got
}

func mustCode(t *testing.T, body apiBody, want int) {
	t.Helper()
	if body.Code != want {
		t.Fatalf("response code=%d msg=%q, want %d", body.Code, body.Message, want)
	}
}

// TestHTTPDisputeLifecycle verifies end-to-end status codes/messages through
// the real router, including every distinguishable rejection reason.
func TestHTTPDisputeLifecycle(t *testing.T) {
	e := newDisputeHTTPEnv(t)
	partyA, partyB, outsider, admin := e.token(t, e.partyA), e.token(t, e.partyB), e.token(t, e.outsider), e.token(t, e.admin)

	// Unauthenticated request is rejected at the middleware.
	if status, body := e.do(t, http.MethodGet, "/api/v1/disputes", "", nil); status != http.StatusUnauthorized || body.Code != constants.CodeUnauthorized {
		t.Fatalf("no-token: status=%d code=%d, want 401/%d", status, body.Code, constants.CodeUnauthorized)
	}

	// Invalid payload (missing required fields) -> 40000.
	_, body := e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/contracts/%d/disputes", e.contractID), partyA, map[string]any{"reason": "x"})
	mustCode(t, body, constants.CodeBadRequest)

	// Outsider files -> not a party.
	_, body = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/contracts/%d/disputes", e.contractID), outsider,
		map[string]any{"reason": "无关方也想发起争议试试看", "claim": "无理由诉求"})
	mustCode(t, body, constants.CodeDisputeNotParty)

	// Party A files the dispute.
	status, body := e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/contracts/%d/disputes", e.contractID), partyA,
		map[string]any{"reason": "乙方延期交付且验收质量不达标", "claim": "要求退还部分款项", "evidence": []string{"a.png"}})
	if status != http.StatusOK {
		t.Fatalf("file status=%d", status)
	}
	mustCode(t, body, constants.CodeSuccess)
	var filed model.Dispute
	if err := json.Unmarshal(body.Data, &filed); err != nil {
		t.Fatal(err)
	}

	// While open: dashboard + contract list expose activeDispute and no ruling.
	openContract := partyContractFromDashboard(t, e, partyA, e.contractID)
	if openContract.ActiveDispute == nil || openContract.ActiveDispute.ID != filed.ID {
		t.Fatalf("dashboard must show activeDispute #%d while open, got %+v", filed.ID, openContract.ActiveDispute)
	}
	if openContract.LastRuling != nil {
		t.Fatalf("dashboard must not show a ruling before adjudication, got %+v", openContract.LastRuling)
	}
	if list := partyContracts(t, e, partyA); len(list) == 0 || list[0].ActiveDispute == nil || list[0].LastRuling != nil {
		t.Fatalf("contract list must show activeDispute/no ruling while open: %+v", list)
	}
	// Outsider must see neither the active dispute nor any verdict.
	outsiderContract := getContract(t, e, outsider, e.contractID)
	if outsiderContract.ActiveDispute != nil || outsiderContract.LastRuling != nil {
		t.Fatalf("outsider must not receive dispute data: %+v", outsiderContract)
	}

	// Duplicate filing by party B -> already open.
	_, body = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/contracts/%d/disputes", e.contractID), partyB,
		map[string]any{"reason": "处理中又来一条争议提交", "claim": "反诉"})
	mustCode(t, body, constants.CodeDisputeAlreadyOpen)

	// Party tries to accept -> not admin.
	_, body = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/accept", filed.ID), partyA, nil)
	mustCode(t, body, constants.CodeDisputeNotAdmin)

	// Admin rules before accepting -> illegal transition.
	_, body = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/rule", filed.ID), admin,
		map[string]any{"responsibility": "乙方主责", "rulingParty": "party_b", "refundAmount": 1000, "opinion": "裁决意见需要足够详细"})
	mustCode(t, body, constants.CodeDisputeIllegalTransition)

	// Invalid ruling payload (bad enum + missing opinion) -> validation 40000.
	_, body = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/rule", filed.ID), admin,
		map[string]any{"responsibility": "x", "rulingParty": "whoever", "refundAmount": 1000})
	mustCode(t, body, constants.CodeBadRequest)

	// Outsider cannot read the dispute.
	_, body = e.do(t, http.MethodGet, fmt.Sprintf("/api/v1/disputes/%d", filed.ID), outsider, nil)
	mustCode(t, body, constants.CodeDisputeNotParty)

	// Admin requests supplement; outsider supplement rejected; party supplement accepted.
	_, body = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/request-supplement", filed.ID), admin,
		map[string]any{"note": "请补充验收沟通记录"})
	mustCode(t, body, constants.CodeSuccess)
	_, body = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/supplement", filed.ID), outsider,
		map[string]any{"content": "无关方补充材料"})
	mustCode(t, body, constants.CodeDisputeNotParty)
	_, body = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/supplement", filed.ID), partyB,
		map[string]any{"content": "已补充验收沟通截图", "attachments": []string{"p.png"}})
	mustCode(t, body, constants.CodeSuccess)

	// Admin rules.
	_, body = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/rule", filed.ID), admin,
		map[string]any{"responsibility": "乙方承担主要责任", "rulingParty": "party_b", "refundAmount": 20000, "opinion": "依据证据裁决部分退款"})
	mustCode(t, body, constants.CodeSuccess)

	// Closed: re-rule and supplement are both rejected with the closed code.
	_, body = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/rule", filed.ID), admin,
		map[string]any{"responsibility": "再来一次裁决", "rulingParty": "shared", "refundAmount": 1, "opinion": "重复裁决"})
	mustCode(t, body, constants.CodeDisputeClosed)
	_, body = e.do(t, http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/supplement", filed.ID), partyA,
		map[string]any{"content": "关闭后还想补充材料"})
	mustCode(t, body, constants.CodeDisputeClosed)

	// Contract detail no longer reports an active dispute after closing.
	_, body = e.do(t, http.MethodGet, fmt.Sprintf("/api/v1/contracts/%d", e.contractID), partyA, nil)
	mustCode(t, body, constants.CodeSuccess)
	var contract model.Contract
	if err := json.Unmarshal(body.Data, &contract); err != nil {
		t.Fatal(err)
	}
	if contract.ActiveDispute != nil {
		t.Fatalf("activeDispute should be null after ruling, got %+v", contract.ActiveDispute)
	}
	if contract.LastRuling == nil || contract.LastRuling.ID != filed.ID {
		t.Fatalf("contract detail must keep lastRuling #%d after closing, got %+v", filed.ID, contract.LastRuling)
	}
	if contract.LastRuling.RefundAmount == nil || *contract.LastRuling.RefundAmount != 20000 {
		t.Fatalf("lastRuling refund=%v want 20000", contract.LastRuling.RefundAmount)
	}

	// Workbench + list: the verdict persists while the open marker is gone.
	closedContract := partyContractFromDashboard(t, e, partyA, e.contractID)
	if closedContract.ActiveDispute != nil {
		t.Fatalf("dashboard activeDispute must clear after ruling, got %+v", closedContract.ActiveDispute)
	}
	if closedContract.LastRuling == nil || closedContract.LastRuling.ID != filed.ID {
		t.Fatalf("dashboard card must keep showing lastRuling #%d, got %+v", filed.ID, closedContract.LastRuling)
	}
	if list := partyContracts(t, e, partyB); len(list) == 0 || list[0].ActiveDispute != nil || list[0].LastRuling == nil || list[0].LastRuling.ID != filed.ID {
		t.Fatalf("party B contract list must show the verdict and no active dispute: %+v", list)
	}
}

func partyContractFromDashboard(t *testing.T, e *disputeHTTPEnv, token string, contractID uint) model.Contract {
	t.Helper()
	_, body := e.do(t, http.MethodGet, "/api/v1/dashboard", token, nil)
	mustCode(t, body, constants.CodeSuccess)
	var dash struct {
		MyContracts []model.Contract `json:"myContracts"`
	}
	if err := json.Unmarshal(body.Data, &dash); err != nil {
		t.Fatal(err)
	}
	for _, c := range dash.MyContracts {
		if c.ID == contractID {
			return c
		}
	}
	t.Fatalf("contract %d not found in dashboard", contractID)
	return model.Contract{}
}

func partyContracts(t *testing.T, e *disputeHTTPEnv, token string) []model.Contract {
	t.Helper()
	_, body := e.do(t, http.MethodGet, "/api/v1/contracts", token, nil)
	mustCode(t, body, constants.CodeSuccess)
	var list []model.Contract
	if err := json.Unmarshal(body.Data, &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func getContract(t *testing.T, e *disputeHTTPEnv, token string, contractID uint) model.Contract {
	t.Helper()
	_, body := e.do(t, http.MethodGet, fmt.Sprintf("/api/v1/contracts/%d", contractID), token, nil)
	mustCode(t, body, constants.CodeSuccess)
	var c model.Contract
	if err := json.Unmarshal(body.Data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}
