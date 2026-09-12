package model

import "time"

// WaitlistEntry 地块候补认养申请实体。
//
// 状态机（WaitlistStatus）：
//
//	waiting   —— 候补排队中（地块已被认养，市民申请候补）
//	invited   —— 地块释放后，最早申请者获得优先认养资格（认养或取消前一直有效）
//	adopted   —— 受邀人在资格有效期内完成认养（终态）
//	cancelled —— 申请人主动取消（终态；同一地块同一人可再次申请）
//
// 同一地块同一用户只保留一条“有效申请”（waiting / invited）：
// service 层在事务内先锁地块行（SELECT ... FOR UPDATE）再查重，
// 另由数据库部分唯一索引 waitlist_active_uniq（plot_id, user_id）
// WHERE status IN ('waiting','invited') 兜底（见 database.EnsurePartialIndexes）。
type WaitlistEntry struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	PlotID     uint       `gorm:"not null;index:idx_waitlist_plot" json:"plot_id"`
	Plot       *Plot      `gorm:"foreignKey:PlotID" json:"plot"`
	UserID     uint       `gorm:"not null;index:idx_waitlist_user" json:"user_id"`
	User       *User      `gorm:"foreignKey:UserID" json:"user"`
	Status     string     `gorm:"size:32;not null;default:waiting;index:idx_waitlist_status" json:"status"`
	InvitedAt  *time.Time `json:"invited_at"`
	CanceledAt *time.Time `json:"canceled_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	Note       string     `gorm:"size:512" json:"note"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
