package dto

import (
	"time"

	"github.com/communitygarden/server/internal/model"
)

// CreateWaitlistRequest 申请候补认养（地块已被认养时）。
type CreateWaitlistRequest struct {
	Note string `json:"note" binding:"omitempty,max=512"`
}

// CancelWaitlistRequest 取消候补申请。
type CancelWaitlistRequest struct {
	Reason string `json:"reason" binding:"omitempty,max=256"`
}

// WaitlistOutDTO 候补申请输出（含申请人/地块摘要，供管理员、认养人查看名单）。
type WaitlistOutDTO struct {
	ID         uint        `json:"id"`
	PlotID     uint        `json:"plot_id"`
	Plot       *PlotOutDTO `json:"plot"`
	UserID     uint        `json:"user_id"`
	User       *UserOutDTO `json:"user"`
	Status     string      `json:"status"`
	Note       string      `json:"note"`
	Rank       int         `json:"rank"` // 当前在排队队列中的位次（0 表示已受邀或不在队列）
	InvitedAt  *time.Time  `json:"invited_at"`
	CanceledAt *time.Time  `json:"canceled_at"`
	CreatedAt  string      `json:"created_at"`
}

// ToWaitlistOutDTO 模型转 DTO（不预填位次与关联实体）。
func ToWaitlistOutDTO(e *model.WaitlistEntry) *WaitlistOutDTO {
	out := &WaitlistOutDTO{
		ID:         e.ID,
		PlotID:     e.PlotID,
		UserID:     e.UserID,
		Status:     e.Status,
		Note:       e.Note,
		InvitedAt:  e.InvitedAt,
		CanceledAt: e.CanceledAt,
		CreatedAt:  e.CreatedAt.Format("2006-01-02 15:04:05"),
	}
	if e.Plot != nil {
		out.Plot = ToPlotOutDTO(e.Plot)
	}
	if e.User != nil {
		out.User = ToUserOutDTO(e.User)
	}
	return out
}

// WaitlistSummaryDTO 地块候补摘要（页面展示候补人数与当前用户状态）。
type WaitlistSummaryDTO struct {
	PlotID       uint            `json:"plot_id"`
	WaitingCount int64           `json:"waiting_count"` // 排队中人数（waiting）
	ActiveCount  int64           `json:"active_count"`  // 有效申请人数（waiting + invited）
	Reserved     bool            `json:"reserved"`      // 是否已有候补者持有优先认养资格
	InvitedToMe  bool            `json:"invited_to_me"` // 受邀资格是否属于当前用户
	Mine         *WaitlistOutDTO `json:"mine"`          // 当前登录用户在该地块的申请（无则 null）
}

// MyWaitlistStatusDTO 我的候补状态（地块列表页批量查询用）。
type MyWaitlistStatusDTO struct {
	PlotID       uint   `json:"plot_id"`
	WaitlistID   uint   `json:"waitlist_id"`
	Status       string `json:"status"`
	Rank         int    `json:"rank"`
	WaitingCount int64  `json:"waiting_count"`
	ActiveCount  int64  `json:"active_count"`
	// Reserved 地块释放后是否已有候补者持有优先认养资格。
	Reserved bool `json:"reserved"`
	// InvitedToMe 当前受邀资格是否属于查询者本人。
	InvitedToMe bool `json:"invited_to_me"`
}
