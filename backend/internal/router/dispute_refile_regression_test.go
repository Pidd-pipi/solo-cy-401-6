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

// mustDo issues one JSON request over TCP and decodes the unified envelope.
func mustDo(t *testing.T, client *http.Client, base, method, path, token string, payload any) apiBody {
	t.Helper()
	var reader io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, base+path, reader)
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

// TestHTTPRegressRefileKeepsRulingAndOpen drives the real router over TCP and
// asserts the workbench card keeps BOTH the last ruling and a newly opened
// dispute, that the last ruling is only ever a closed record, and that an
// outsider receives neither. Repeatable: every run builds a fresh in-memory DB.
func TestHTTPRegressRefileKeepsRulingAndOpen(t *testing.T) {
	e := newDisputeHTTPEnv(t)
	srv := httptest.NewServer(e.engine)
	defer srv.Close()

	partyA := e.token(t, e.partyA)
	partyB := e.token(t, e.partyB)
	outsider := e.token(t, e.outsider)
	admin := e.token(t, e.admin)
	client := srv.Client()

	call := func(method, path, token string, payload any) apiBody {
		t.Helper()
		b := mustDo(t, client, srv.URL, method, path, token, payload)
		return b
	}

	// File -> accept -> rule (closed #1).
	body := call(http.MethodPost, fmt.Sprintf("/api/v1/contracts/%d/disputes", e.contractID), partyA,
		map[string]any{"reason": "首次争议：乙方延期交付且质量不达标", "claim": "要求部分退款"})
	mustCode(t, body, constants.CodeSuccess)
	var first model.Dispute
	if err := json.Unmarshal(body.Data, &first); err != nil {
		t.Fatal(err)
	}
	mustCode(t, call(http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/accept", first.ID), admin, nil), constants.CodeSuccess)
	mustCode(t, call(http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/rule", first.ID), admin,
		map[string]any{"responsibility": "乙方承担主要责任", "rulingParty": "party_b", "refundAmount": 15000, "opinion": "裁决退款15000元并关闭争议"}), constants.CodeSuccess)

	// Re-file a new open dispute (#2).
	body = call(http.MethodPost, fmt.Sprintf("/api/v1/contracts/%d/disputes", e.contractID), partyB,
		map[string]any{"reason": "对退款执行进度有异议需要平台跟进", "claim": "督促尽快执行退款"})
	mustCode(t, body, constants.CodeSuccess)
	var second model.Dispute
	if err := json.Unmarshal(body.Data, &second); err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatalf("re-file must create a new dispute record")
	}

	// Existing ruling/permission behavior must be unchanged.
	mustCode(t, call(http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/rule", second.ID), partyA,
		map[string]any{"responsibility": "越权裁决", "rulingParty": "shared", "refundAmount": 1, "opinion": "越权"}), constants.CodeDisputeNotAdmin)
	mustCode(t, call(http.MethodPost, fmt.Sprintf("/api/v1/disputes/%d/rule", first.ID), admin,
		map[string]any{"responsibility": "重复裁决", "rulingParty": "shared", "refundAmount": 1, "opinion": "重复裁决"}), constants.CodeDisputeClosed)

	dashboardCard := func(token string) model.Contract {
		t.Helper()
		b := call(http.MethodGet, "/api/v1/dashboard", token, nil)
		mustCode(t, b, constants.CodeSuccess)
		var dash struct {
			MyContracts []model.Contract `json:"myContracts"`
		}
		if err := json.Unmarshal(b.Data, &dash); err != nil {
			t.Fatal(err)
		}
		for _, c := range dash.MyContracts {
			if c.ID == e.contractID {
				return c
			}
		}
		t.Fatalf("contract %d missing from dashboard", e.contractID)
		return model.Contract{}
	}

	// Both parties: card keeps the closed verdict AND shows the open dispute.
	for _, tok := range []string{partyA, partyB} {
		card := dashboardCard(tok)
		if card.ActiveDispute == nil || card.ActiveDispute.ID != second.ID || card.ActiveDispute.Status != constants.DisputeSubmitted {
			t.Fatalf("card activeDispute must be open #%d, got %+v", second.ID, card.ActiveDispute)
		}
		if card.LastRuling == nil || card.LastRuling.ID != first.ID {
			t.Fatalf("card lastRuling must retain closed #%d, got %+v", first.ID, card.LastRuling)
		}
		if card.LastRuling.Status != constants.DisputeRuled {
			t.Fatalf("lastRuling must only be a closed record, got %s", card.LastRuling.Status)
		}
		if card.LastRuling.RefundAmount == nil || *card.LastRuling.RefundAmount != 15000 {
			t.Fatalf("lastRuling refund=%v want 15000", card.LastRuling.RefundAmount)
		}
	}

	// Outsider: contract detail hides both fields.
	b := call(http.MethodGet, fmt.Sprintf("/api/v1/contracts/%d", e.contractID), outsider, nil)
	mustCode(t, b, constants.CodeSuccess)
	var oc model.Contract
	if err := json.Unmarshal(b.Data, &oc); err != nil {
		t.Fatal(err)
	}
	if oc.ActiveDispute != nil || oc.LastRuling != nil {
		t.Fatalf("outsider must see neither active dispute nor ruling: active=%+v last=%+v", oc.ActiveDispute, oc.LastRuling)
	}
}
