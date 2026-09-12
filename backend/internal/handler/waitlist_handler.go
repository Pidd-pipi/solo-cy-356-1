package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/communitygarden/server/internal/constants"
	"github.com/communitygarden/server/internal/dto"
	"github.com/communitygarden/server/internal/middleware"
	"github.com/communitygarden/server/internal/service"
	"github.com/communitygarden/server/internal/util"
)

// WaitlistHandler 候补认养接口。
type WaitlistHandler struct {
	waitlistService *service.WaitlistService
	audit           middleware.AuditWriter
}

// NewWaitlistHandler 构造候补认养接口。
func NewWaitlistHandler(waitlistService *service.WaitlistService, audit middleware.AuditWriter) *WaitlistHandler {
	return &WaitlistHandler{waitlistService: waitlistService, audit: audit}
}

// Apply 市民对已被认养的地块申请候补。
// POST /api/v1/plots/:id/waitlist
func (h *WaitlistHandler) Apply(c *gin.Context) {
	plotID, ok := parsePlotID(c)
	if !ok {
		return
	}
	var req dto.CreateWaitlistRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			util.Fail(c, http.StatusBadRequest, constants.CodeValidationFailed, constants.ErrorText[constants.CodeValidationFailed]+": "+err.Error())
			return
		}
	}
	claims, _ := util.GetClaims(c)
	entry, err := h.waitlistService.Apply(plotID, claims.UserID, claims.Role, claims.Username, req.Note)
	if err != nil {
		util.FailWithAppError(c, err)
		return
	}
	_ = h.audit.Write(claims.UserID, claims.Username, claims.Role, "APPLY_WAITLIST", "waitlist", strconv.FormatUint(uint64(entry.ID), 10),
		"申请地块 "+strconv.FormatUint(uint64(plotID), 10)+" 候补", c.ClientIP(), util.GetRequestID(c))
	util.OK(c, dto.ToWaitlistOutDTO(entry))
}

// Cancel 申请人取消自己的候补申请。
// DELETE /api/v1/waitlist/:id
func (h *WaitlistHandler) Cancel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		util.Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "路径参数 id 必须为正整数")
		return
	}
	claims, _ := util.GetClaims(c)
	entry, err := h.waitlistService.Cancel(uint(id), claims.UserID, claims.Role)
	if err != nil {
		util.FailWithAppError(c, err)
		return
	}
	_ = h.audit.Write(claims.UserID, claims.Username, claims.Role, "CANCEL_WAITLIST", "waitlist", strconv.FormatUint(uint64(id), 10),
		"取消地块 "+strconv.FormatUint(uint64(entry.PlotID), 10)+" 候补", c.ClientIP(), util.GetRequestID(c))
	util.OK(c, dto.ToWaitlistOutDTO(entry))
}

// ListByPlot 管理员与认养人查看按申请时间排序的候补名单。
// GET /api/v1/plots/:id/waitlist
func (h *WaitlistHandler) ListByPlot(c *gin.Context) {
	plotID, ok := parsePlotID(c)
	if !ok {
		return
	}
	claims, _ := util.GetClaims(c)
	list, total, err := h.waitlistService.ListByPlot(plotID, claims.UserID, claims.Role)
	if err != nil {
		util.FailWithAppError(c, err)
		return
	}
	util.OK(c, gin.H{"list": list, "total": total})
}

// Summary 地块候补人数与当前用户申请状态（页面展示候补人数、当前状态）。
// GET /api/v1/plots/:id/waitlist/summary
func (h *WaitlistHandler) Summary(c *gin.Context) {
	plotID, ok := parsePlotID(c)
	if !ok {
		return
	}
	var viewerID uint
	if claims, ok := util.GetClaims(c); ok {
		viewerID = claims.UserID
	}
	summary, err := h.waitlistService.Summary(plotID, viewerID)
	if err != nil {
		util.FailWithAppError(c, err)
		return
	}
	util.OK(c, summary)
}

// BatchStatus 地块列表页批量获取候补计数与我的申请状态。
// GET /api/v1/waitlist/status?plot_ids=1,2,3
func (h *WaitlistHandler) BatchStatus(c *gin.Context) {
	claims, _ := util.GetClaims(c)
	raw := c.Query("plot_ids")
	plotIDs := make([]uint, 0)
	if raw != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseUint(part, 10, 64)
			if err != nil || id == 0 {
				util.Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "查询参数 plot_ids 必须为逗号分隔的正整数")
				return
			}
			plotIDs = append(plotIDs, uint(id))
		}
	}
	if len(plotIDs) > util.MaxPageSize {
		util.Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "plot_ids 数量不能超过 100")
		return
	}
	list, err := h.waitlistService.BatchStatus(plotIDs, claims.UserID)
	if err != nil {
		util.FailWithAppError(c, err)
		return
	}
	util.OK(c, gin.H{"list": list})
}

// ListMine 当前用户的候补申请列表。
// GET /api/v1/waitlist/mine
func (h *WaitlistHandler) ListMine(c *gin.Context) {
	pq := util.ParsePageQuery(c)
	status := c.Query("status")
	claims, _ := util.GetClaims(c)
	list, total, err := h.waitlistService.ListMine(pq, claims.UserID, status)
	if err != nil {
		util.FailWithAppError(c, err)
		return
	}
	util.OK(c, util.PageResult{List: list, Total: total, Page: pq.Page, PageSize: pq.PageSize})
}

// parsePlotID 解析路径参数 :id（plots 与 waitlist 路由共用）。
func parsePlotID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		util.Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "路径参数 id 必须为正整数")
		return 0, false
	}
	return uint(id), true
}
