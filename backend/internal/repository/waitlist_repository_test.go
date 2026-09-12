package repository

import (
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/communitygarden/server/internal/constants"
	"github.com/communitygarden/server/internal/model"
)

func seedPlot(t *testing.T, db *gorm.DB, code string) *model.Plot {
	t.Helper()
	p := &model.Plot{Name: code, Code: code, Area: 10, SoilType: "loam", Sunlight: "full",
		Latitude: 31.0, Longitude: 121.0, Status: string(constants.PlotStatusAdopted)}
	if err := db.Create(p).Error; err != nil {
		t.Fatalf("seed plot: %v", err)
	}
	return p
}

func TestWaitlistRepository_DuplicateActiveRejected(t *testing.T) {
	db := newTestDB(t)
	repo := NewWaitlistRepository(db)
	u := seedUser(t, db, "w1", "citizen")
	p := seedPlot(t, db, "P-WL-DUP")

	first := &model.WaitlistEntry{PlotID: p.ID, UserID: u.ID, Status: string(constants.WaitlistWaiting)}
	if err := repo.Create(first); err != nil {
		t.Fatalf("create first: %v", err)
	}

	// 同一地块同一用户的第二条有效申请必须被部分唯一索引拒绝
	dup := &model.WaitlistEntry{PlotID: p.ID, UserID: u.ID, Status: string(constants.WaitlistInvited)}
	if err := repo.Create(dup); err == nil {
		t.Fatalf("expected unique constraint error for duplicate active entry")
	}

	// 第一条变为终态后，可再次插入有效申请
	if err := db.Model(&model.WaitlistEntry{}).Where("id = ?", first.ID).
		Update("status", string(constants.WaitlistCancelled)).Error; err != nil {
		t.Fatalf("cancel first: %v", err)
	}
	again := &model.WaitlistEntry{PlotID: p.ID, UserID: u.ID, Status: string(constants.WaitlistWaiting)}
	if err := repo.Create(again); err != nil {
		t.Fatalf("create after terminal should succeed: %v", err)
	}
}

func TestWaitlistRepository_OrderingAndCount(t *testing.T) {
	db := newTestDB(t)
	repo := NewWaitlistRepository(db)
	u1 := seedUser(t, db, "o1", "citizen")
	u2 := seedUser(t, db, "o2", "citizen")
	u3 := seedUser(t, db, "o3", "citizen")
	p := seedPlot(t, db, "P-WL-ORD")

	mk := func(uid uint, status string) *model.WaitlistEntry {
		e := &model.WaitlistEntry{PlotID: p.ID, UserID: uid, Status: status}
		if err := repo.Create(e); err != nil {
			t.Fatalf("create entry: %v", err)
		}
		return e
	}
	e1 := mk(u1.ID, string(constants.WaitlistWaiting))
	e2 := mk(u2.ID, string(constants.WaitlistWaiting))
	e3 := mk(u3.ID, string(constants.WaitlistWaiting))
	// e1 受邀
	if err := db.Model(&model.WaitlistEntry{}).Where("id = ?", e1.ID).
		Update("status", string(constants.WaitlistInvited)).Error; err != nil {
		t.Fatalf("invite e1: %v", err)
	}

	// 有效申请按申请时间升序：e1, e2, e3
	active, err := repo.ListActiveByPlot(p.ID)
	if err != nil {
		t.Fatalf("ListActiveByPlot: %v", err)
	}
	if len(active) != 3 || active[0].ID != e1.ID || active[1].ID != e2.ID || active[2].ID != e3.ID {
		t.Fatalf("active order = %v, want %d,%d,%d", ids(active), e1.ID, e2.ID, e3.ID)
	}

	// 全部候补名单（有效申请）按申请时间升序：e1(invited), e2, e3
	all, err := repo.ListByPlot(p.ID)
	if err != nil {
		t.Fatalf("ListByPlot: %v", err)
	}
	if len(all) != 3 || all[0].ID != e1.ID || all[1].ID != e2.ID || all[2].ID != e3.ID {
		t.Fatalf("all order = %v, want %d,%d,%d", ids(all), e1.ID, e2.ID, e3.ID)
	}
	// 预载申请人
	if all[0].User == nil || all[0].User.ID != u1.ID {
		t.Fatalf("preloaded user missing: %+v", all[0].User)
	}

	// 终态申请不出现在名单中：e3 取消后名单剩 2 条
	if err := db.Model(&model.WaitlistEntry{}).Where("id = ?", e3.ID).
		Update("status", string(constants.WaitlistCancelled)).Error; err != nil {
		t.Fatalf("cancel e3: %v", err)
	}
	all, err = repo.ListByPlot(p.ID)
	if err != nil {
		t.Fatalf("ListByPlot after cancel: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("active list len=%d, want 2", len(all))
	}
	// 恢复 e3 排队状态，继续后续计数校验
	if err := db.Model(&model.WaitlistEntry{}).Where("id = ?", e3.ID).
		Update("status", string(constants.WaitlistWaiting)).Error; err != nil {
		t.Fatalf("restore e3: %v", err)
	}

	// 最早排队者 = e2
	tx := db.Begin()
	defer tx.Rollback()
	earliest, err := repo.FindEarliestWaitingForUpdate(tx, p.ID)
	if err != nil {
		t.Fatalf("FindEarliestWaitingForUpdate: %v", err)
	}
	if earliest.ID != e2.ID {
		t.Fatalf("earliest = %d, want %d", earliest.ID, e2.ID)
	}

	// 计数：waiting=2, active=3
	waiting, activeCount, err := repo.CountByPlot(p.ID)
	if err != nil {
		t.Fatalf("CountByPlot: %v", err)
	}
	if waiting != 2 || activeCount != 3 {
		t.Fatalf("waiting=%d active=%d, want 2/3", waiting, activeCount)
	}

	// 分组计数
	wm, err := repo.CountWaitingGrouped([]uint{p.ID})
	if err != nil || wm[p.ID] != 2 {
		t.Fatalf("CountWaitingGrouped = %v err=%v", wm, err)
	}

	// 有效申请查重
	if _, err := repo.FindActiveByPlotAndUser(tx, p.ID, u1.ID); err != nil {
		t.Fatalf("FindActiveByPlotAndUser u1: %v", err)
	}
	if _, err := repo.FindActiveByPlotAndUser(tx, p.ID, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func ids(list []model.WaitlistEntry) []uint {
	out := make([]uint, 0, len(list))
	for _, e := range list {
		out = append(out, e.ID)
	}
	return out
}
