package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/model"
)

// TestHTTPDisputeOverTCP drives the SAME router over a real TCP listener with a
// real http.Client (not in-process ServeHTTP), proving the verdict survives on
// the workbench card across the before/after ruling states.
func TestHTTPDisputeOverTCP(t *testing.T) {
	e := newDisputeHTTPEnv(t)
	srv := httptest.NewServer(e.engine)
	defer srv.Close()

	partyA := e.token(t, e.partyA)
	admin := e.token(t, e.admin)
	client := srv.Client()

	call := func(method, path, token string, payload any) apiBody {
		t.Helper()
		var reader io.Reader
		if payload != nil {
			raw, _ := json.Marshal(payload)
			reader = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, srv.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var got apiBody
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode %s: %v body=%s", path, err, raw)
		}
		return got
	}
	dashboardCard := func() model.Contract {
		t.Helper()
		body := call(http.MethodGet, "/api/v1/dashboard", partyA, nil)
		mustCode(t, body, constants.CodeSuccess)
		var dash struct {
			MyContracts []model.Contract `json:"myContracts"`
		}
		if err := json.Unmarshal(body.Data, &dash); err != nil {
			t.Fatal(err)
		}
		for _, c := range dash.MyContracts {
			if c.ID == e.contractID {
				return c
			}
		}
		t.Fatalf("contract %d missing from dashboard over TCP", e.contractID)
		return model.Contract{}
	}

	// File over TCP.
	b := call(http.MethodPost, fmt.Sprintf("/api/v1/contracts/%d/disputes", e.contractID), partyA,
		map[string]any{"reason": "TCP 真实接口：乙方延期交付", "claim": "要求部分退款", "evidence": []string{"e1.png"}})
	mustCode(t, b, constants.CodeSuccess)
	var d model.Dispute
	if err := json.Unmarshal(b.Data, &d); err != nil {
		t.Fatal(err)
	}

	// Before adjudication: card shows the open dispute, no verdict.
	card := dashboardCard()
	if card.ActiveDispute == nil || card.ActiveDispute.ID != d.ID || card.LastRuling != nil {
		t.Fatalf("before ruling card wrong: active=%+v last=%+v", card.ActiveDispute, card.LastRuling)
	}

	// Repeated file is rejected over TCP with the distinct code.
	b = call(http.MethodPost, fmt.Sprintf("/api/v1/contracts/%d/disputes", e.contractID), partyA,
		map[string]any{"reason": "重复提交争议内容", "claim": "重复诉求"})
	mustCode(t, b, constants.CodeDisputeAlreadyOpen)

	// Non-admin cannot rule over TCP.
	b = call(http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/rule", d.ID), partyA,
		map[string]any{"responsibility": "越权裁决", "rulingParty": "shared", "refundAmount": 1, "opinion": "越权"})
	mustCode(t, b, constants.CodeDisputeNotAdmin)

	// Rule before accept is an illegal transition.
	b = call(http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/rule", d.ID), admin,
		map[string]any{"responsibility": "未受理直接裁决", "rulingParty": "shared", "refundAmount": 1, "opinion": "非法跳转"})
	mustCode(t, b, constants.CodeDisputeIllegalTransition)

	// Accept + rule over TCP.
	b = call(http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/accept", d.ID), admin, nil)
	mustCode(t, b, constants.CodeSuccess)
	b = call(http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/rule", d.ID), admin,
		map[string]any{"responsibility": "TCP 责任划分：乙方主责", "rulingParty": "party_b", "refundAmount": 18888, "opinion": "TCP 处理意见"})
	mustCode(t, b, constants.CodeSuccess)

	// Repeat ruling rejected; supplement on a closed record rejected (no fake verdicts).
	b = call(http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/rule", d.ID), admin,
		map[string]any{"responsibility": "重复裁决尝试", "rulingParty": "shared", "refundAmount": 1, "opinion": "再来一次"})
	mustCode(t, b, constants.CodeDisputeClosed)
	b = call(http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/supplement", d.ID), partyA,
		map[string]any{"content": "已关闭仍想补充材料"})
	mustCode(t, b, constants.CodeDisputeClosed)

	// After adjudication: card keeps the verdict (and shows no active dispute).
	card = dashboardCard()
	if card.ActiveDispute != nil {
		t.Fatalf("after ruling activeDispute must be nil, got %+v", card.ActiveDispute)
	}
	if card.LastRuling == nil || card.LastRuling.ID != d.ID {
		t.Fatalf("after ruling the workbench card must retain lastRuling #%d, got %+v", d.ID, card.LastRuling)
	}
	if card.LastRuling.RefundAmount == nil || *card.LastRuling.RefundAmount != 18888 || card.LastRuling.RulingParty != "party_b" {
		t.Fatalf("card lastRuling fields wrong: %+v", card.LastRuling)
	}
}
