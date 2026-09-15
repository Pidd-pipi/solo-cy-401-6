package handler

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/middleware"
	"github.com/gigmatch/gigmatch/internal/service"
	"github.com/gigmatch/gigmatch/internal/util"
)

// DisputeHandler exposes contract-dispute endpoints.
type DisputeHandler struct {
	svc    *service.DisputeService
	logger *slog.Logger
}

// NewDisputeHandler builds a DisputeHandler.
func NewDisputeHandler(svc *service.DisputeService, logger *slog.Logger) *DisputeHandler {
	return &DisputeHandler{svc: svc, logger: logger}
}

// File handles POST /contracts/:id/disputes — a party files a dispute.
func (h *DisputeHandler) File(c *gin.Context) {
	contractID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req dto.CreateDisputeRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	u := middleware.GetCurrentUser(c)
	d, err := h.svc.File(contractID, u.ID, u.Name, u.Role, req.Reason, req.Claim, req.Evidence)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, d)
}

// ListByContract handles GET /contracts/:id/disputes.
func (h *DisputeHandler) ListByContract(c *gin.Context) {
	contractID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	u := middleware.GetCurrentUser(c)
	list, err := h.svc.ListByContract(contractID, u.ID, u.Role)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, list)
}

// Get handles GET /disputes/:id.
func (h *DisputeHandler) Get(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	u := middleware.GetCurrentUser(c)
	d, err := h.svc.Get(id, u.ID, u.Role)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, d)
}

// List handles GET /disputes: administrators see the full queue, parties see
// only disputes on their own contracts.
func (h *DisputeHandler) List(c *gin.Context) {
	u := middleware.GetCurrentUser(c)
	var (
		list any
		err  error
	)
	if u.Role == constants.RoleAdmin {
		list, err = h.svc.ListAll(u.Role)
	} else {
		list, err = h.svc.ListByParty(u.ID)
	}
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, list)
}

// Accept handles POST /disputes/:id/accept (admin).
func (h *DisputeHandler) Accept(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	u := middleware.GetCurrentUser(c)
	d, err := h.svc.Accept(id, u.ID, u.Name, u.Role)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, d)
}

// RequestSupplement handles POST /disputes/:id/request-supplement (admin).
func (h *DisputeHandler) RequestSupplement(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req dto.RequestSupplementRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	u := middleware.GetCurrentUser(c)
	d, err := h.svc.RequestSupplement(id, u.ID, u.Name, u.Role, req.Note)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, d)
}

// Supplement handles POST /disputes/:id/supplement (party).
func (h *DisputeHandler) Supplement(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req dto.SupplementRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	u := middleware.GetCurrentUser(c)
	d, err := h.svc.Supplement(id, u.ID, u.Name, req.Content, req.Attachments)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, d)
}

// Rule handles POST /disputes/:id/rule (admin).
func (h *DisputeHandler) Rule(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req dto.RuleDisputeRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	u := middleware.GetCurrentUser(c)
	d, err := h.svc.Rule(id, u.ID, u.Name, u.Role, service.RulingInput{
		Responsibility: req.Responsibility,
		RulingParty:    req.RulingParty,
		RefundAmount:   req.RefundAmount,
		Opinion:        req.Opinion,
	})
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, d)
}
