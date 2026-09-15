package service

import (
	"testing"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/repository"
)

// Refile-after-ruling regression coverage.
//
// A dispute that was ruled stays on record as the "last ruling", while a freshly
// filed dispute occupies the "active/open" slot. The workbench must expose BOTH
// at the same time, the last ruling must only ever be a closed (ruled) record,
// and an outsider must receive neither.

// driveToRuled files a dispute for complainant, then accepts and rules it.
func driveToRuled(t *testing.T, e *disputeEnv, complainant uint, complainantName, role string, refund float64, party string) uint {
	t.Helper()
	d, err := e.svc.File(e.contractID, complainant, complainantName, role,
		"首次争议：对方未按约定履行合同义务", "要求退款及补偿", nil)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if _, err := e.svc.Accept(d.ID, e.admin, "争议管理员", constants.RoleAdmin); err != nil {
		t.Fatalf("accept: %v", err)
	}
	in := e.validRule()
	in.RefundAmount = refund
	in.RulingParty = party
	if _, err := e.svc.Rule(d.ID, e.admin, "争议管理员", constants.RoleAdmin, in); err != nil {
		t.Fatalf("rule: %v", err)
	}
	return d.ID
}

// TestRegressWorkbenchKeepsRulingAndOpenDispute verifies the dashboard contract
// for a party simultaneously carries lastRuling (closed #1) and activeDispute
// (open #2) after re-filing.
func TestRegressWorkbenchKeepsRulingAndOpenDispute(t *testing.T) {
	e := newDisputeEnv(t)
	firstID := driveToRuled(t, e, e.partyA, "争议甲方", constants.RoleRequester, 15000, "party_b")

	// Re-file a NEW open dispute on the same contract (allowed after closing).
	second, err := e.svc.File(e.contractID, e.partyB, "争议乙方", constants.RoleFreelancer,
		"对退款执行进度有异议需要平台跟进处理", "督促尽快执行退款", nil)
	if err != nil {
		t.Fatalf("re-file after ruling: %v", err)
	}
	if second.ID == firstID {
		t.Fatalf("re-file must create a new dispute record")
	}

	// Build the same dashboard service the handler uses.
	reqRepo := repository.NewRequirementRepository(e.db)
	bidRepo := repository.NewBidRepository(e.db)
	contractRepo := repository.NewContractRepository(e.db)
	disputeRepo := repository.NewDisputeRepository(e.db)
	dash := NewDashboardService(reqRepo, bidRepo, contractRepo, disputeRepo, discardLogger())

	for _, viewer := range []uint{e.partyA, e.partyB} {
		data, err := dash.Get(viewer)
		if err != nil {
			t.Fatalf("dashboard get: %v", err)
		}
		var cardID int
		var found bool
		for i := range data.MyContracts {
			if data.MyContracts[i].ID == e.contractID {
				cardID = i
				found = true
			}
		}
		if !found {
			t.Fatalf("contract %d missing from party %d workbench", e.contractID, viewer)
		}
		card := data.MyContracts[cardID]

		// Both cues must coexist (not be mutually exclusive).
		if card.ActiveDispute == nil || card.ActiveDispute.ID != second.ID {
			t.Fatalf("party %d activeDispute must be open #%d, got %+v", viewer, second.ID, card.ActiveDispute)
		}
		if card.LastRuling == nil || card.LastRuling.ID != firstID {
			t.Fatalf("party %d lastRuling must retain closed #%d, got %+v", viewer, firstID, card.LastRuling)
		}
		if card.LastRuling.Status != constants.DisputeRuled {
			t.Fatalf("lastRuling must be a closed record, got status=%s", card.LastRuling.Status)
		}
		if card.LastRuling.RefundAmount == nil || *card.LastRuling.RefundAmount != 15000 {
			t.Fatalf("lastRuling refund=%v want 15000", card.LastRuling.RefundAmount)
		}
		if data.Counts["openDisputes"] != 1 {
			t.Fatalf("openDisputes count=%d want 1", data.Counts["openDisputes"])
		}
	}
}

// TestRegressLastRulingOnlyClosed verifies across open/closed transitions that
// lastRuling never references an in-process dispute.
func TestRegressLastRulingOnlyClosed(t *testing.T) {
	e := newDisputeEnv(t)

	// Before any ruling: no lastRuling even while a dispute is open.
	d1, err := e.svc.File(e.contractID, e.partyA, "争议甲方", constants.RoleRequester,
		"首次争议原因说明", "首次诉求", nil)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	c, err := e.contractSv.Get(e.contractID)
	if err != nil {
		t.Fatal(err)
	}
	if c.LastRuling != nil || c.ActiveDispute == nil || c.ActiveDispute.ID != d1.ID {
		t.Fatalf("while open: active=%+v last=%+v", c.ActiveDispute, c.LastRuling)
	}

	// Accept (still open) must not surface as a ruling.
	if _, err := e.svc.Accept(d1.ID, e.admin, "争议管理员", constants.RoleAdmin); err != nil {
		t.Fatalf("accept: %v", err)
	}
	c, _ = e.contractSv.Get(e.contractID)
	if c.LastRuling != nil {
		t.Fatalf("an accepted (open) dispute must never be a ruling: %+v", c.LastRuling)
	}

	// Rule it: now lastRuling is set, active cleared.
	if _, err := e.svc.Rule(d1.ID, e.admin, "争议管理员", constants.RoleAdmin, e.validRule()); err != nil {
		t.Fatalf("rule: %v", err)
	}
	c, _ = e.contractSv.Get(e.contractID)
	if c.LastRuling == nil || c.LastRuling.ID != d1.ID || c.ActiveDispute != nil {
		t.Fatalf("after rule: active=%+v last=%+v", c.ActiveDispute, c.LastRuling)
	}

	// Re-file: active = new open dispute; lastRuling stays the OLD closed record.
	d2, err := e.svc.File(e.contractID, e.partyB, "争议乙方", constants.RoleFreelancer,
		"再次争议原因说明", "再次诉求", nil)
	if err != nil {
		t.Fatalf("re-file: %v", err)
	}
	c, _ = e.contractSv.Get(e.contractID)
	if c.ActiveDispute == nil || c.ActiveDispute.ID != d2.ID {
		t.Fatalf("new dispute must be active, got %+v", c.ActiveDispute)
	}
	if c.LastRuling == nil || c.LastRuling.ID != d1.ID || c.LastRuling.Status != constants.DisputeRuled {
		t.Fatalf("lastRuling must stay closed #%d, got %+v", d1.ID, c.LastRuling)
	}
}

// TestRegressOutsiderSeesNeitherRulingNorDispute verifies the workbench of a user
// who is not a party never carries this contract's dispute data (and a direct
// contract view hides both fields too).
func TestRegressOutsiderSeesNeitherRulingNorDispute(t *testing.T) {
	e := newDisputeEnv(t)
	firstID := driveToRuled(t, e, e.partyA, "争议甲方", constants.RoleRequester, 15000, "party_b")
	if _, err := e.svc.File(e.contractID, e.partyB, "争议乙方", constants.RoleFreelancer,
		"再次争议需要平台跟进处理", "督促执行", nil); err != nil {
		t.Fatalf("re-file: %v", err)
	}

	reqRepo := repository.NewRequirementRepository(e.db)
	bidRepo := repository.NewBidRepository(e.db)
	contractRepo := repository.NewContractRepository(e.db)
	disputeRepo := repository.NewDisputeRepository(e.db)
	dash := NewDashboardService(reqRepo, bidRepo, contractRepo, disputeRepo, discardLogger())

	data, err := dash.Get(e.outsider)
	if err != nil {
		t.Fatalf("dashboard get: %v", err)
	}
	for i := range data.MyContracts {
		if data.MyContracts[i].ID == e.contractID {
			t.Fatalf("outsider workbench must not include the disputed contract at all")
		}
	}

	// Direct contract fetch through the viewer-aware service hides both fields.
	c, err := e.contractSv.GetForViewer(e.contractID, e.outsider, constants.RoleFreelancer)
	if err != nil {
		t.Fatal(err)
	}
	if c.ActiveDispute != nil || c.LastRuling != nil {
		t.Fatalf("outsider must see neither active dispute nor ruling: active=%+v last=%+v", c.ActiveDispute, c.LastRuling)
	}

	// Admin still sees both (so the fix did not weaken admin visibility).
	adminView, err := e.contractSv.GetForViewer(e.contractID, e.admin, constants.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if adminView.ActiveDispute == nil || adminView.LastRuling == nil || adminView.LastRuling.ID != firstID {
		t.Fatalf("admin must still see active dispute and last ruling: active=%+v last=%+v", adminView.ActiveDispute, adminView.LastRuling)
	}
}
