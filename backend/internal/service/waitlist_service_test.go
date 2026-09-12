package service

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/communitygarden/server/internal/constants"
	"github.com/communitygarden/server/internal/model"
	"github.com/communitygarden/server/internal/repository"
	"github.com/communitygarden/server/internal/util"
)

// newWaitlistFixture 构造地块 + 候补服务（钩子互相注入，与 main.go 装配一致）。
func newWaitlistFixture(t *testing.T, db *gorm.DB) (*PlotService, *WaitlistService, repository.WaitlistRepository) {
	t.Helper()
	plotRepo := repository.NewPlotRepository(db)
	waitRepo := repository.NewWaitlistRepository(db)
	plotSvc := NewPlotService(plotRepo, db, testLogger())
	waitSvc := NewWaitlistService(waitRepo, plotRepo, db, testLogger())
	plotSvc.SetWaitlistHooks(waitSvc, waitSvc)
	return plotSvc, waitSvc, waitRepo
}

// adoptPlotInDB 直接将地块置为已认养（绕过 service，构造“地块已被认养”的初始状态）。
func adoptPlotInDB(t *testing.T, db *gorm.DB, plotID, ownerID uint) {
	t.Helper()
	if err := db.Model(&model.Plot{}).Where("id = ?", plotID).
		Updates(map[string]interface{}{"status": "adopted", "adopter_id": ownerID}).Error; err != nil {
		t.Fatalf("adopt plot in db: %v", err)
	}
}

// setEntryCreatedAt 固定申请时间，保证排序确定性。
func setEntryCreatedAt(t *testing.T, db *gorm.DB, id uint, at time.Time) {
	t.Helper()
	if err := db.Model(&model.WaitlistEntry{}).Where("id = ?", id).Update("created_at", at).Error; err != nil {
		t.Fatalf("set created_at: %v", err)
	}
}

func TestWaitlistService_Apply(t *testing.T) {
	db := newTestServiceDB(t)
	_, waitSvc, _ := newWaitlistFixture(t, db)
	owner := newTestUser(t, db, "owner", "citizen")
	applicant := newTestUser(t, db, "applicant", "citizen")
	plot := newTestPlot(t, db, "P-WL-APPLY", "available", nil)
	adoptPlotInDB(t, db, plot.ID, owner.ID)

	// 首次申请成功
	entry, err := waitSvc.Apply(plot.ID, applicant.ID, "citizen", "applicant", "想种番茄")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if entry.Status != string(constants.WaitlistWaiting) {
		t.Fatalf("status=%s, want waiting", entry.Status)
	}

	// 同一地块同一人重复申请被拒绝
	if _, err := waitSvc.Apply(plot.ID, applicant.ID, "citizen", "applicant", ""); err == nil {
		t.Fatalf("expected duplicate apply to be rejected")
	} else if ae, ok := err.(*util.AppError); !ok || ae.Code != constants.CodeWaitlistDuplicate {
		t.Fatalf("duplicate apply err=%v, want code %d", err, constants.CodeWaitlistDuplicate)
	}

	// 当前认养人不能申请自己地块的候补
	if _, err := waitSvc.Apply(plot.ID, owner.ID, "citizen", "owner", ""); err == nil {
		t.Fatalf("owner should not be allowed to apply for own plot")
	}

	// 空闲且无受邀资格的地块不接受候补
	freePlot := newTestPlot(t, db, "P-WL-FREE", "available", nil)
	if _, err := waitSvc.Apply(freePlot.ID, applicant.ID, "citizen", "applicant", ""); err == nil {
		t.Fatalf("apply on available plot should be rejected")
	}

	// 不存在的地块 -> 404
	if _, err := waitSvc.Apply(99999, applicant.ID, "citizen", "applicant", ""); err == nil {
		t.Fatalf("apply on missing plot should fail")
	}
}

func TestWaitlistService_ReleaseInvitesEarliest(t *testing.T) {
	db := newTestServiceDB(t)
	plotSvc, waitSvc, waitRepo := newWaitlistFixture(t, db)
	owner := newTestUser(t, db, "owner", "farmer")
	u1 := newTestUser(t, db, "u1", "citizen")
	u2 := newTestUser(t, db, "u2", "citizen")
	u3 := newTestUser(t, db, "u3", "citizen")
	ownerID := owner.ID
	plot := newTestPlot(t, db, "P-WL-REL", "harvested", &ownerID)

	// 三人按时间顺序申请（harvested 待释放地块同样允许候补）
	e1, err := waitSvc.Apply(plot.ID, u1.ID, "citizen", "u1", "")
	if err != nil {
		t.Fatalf("apply u1: %v", err)
	}
	setEntryCreatedAt(t, db, e1.ID, time.Now().Add(-3*time.Hour))
	e2, _ := waitSvc.Apply(plot.ID, u2.ID, "citizen", "u2", "")
	setEntryCreatedAt(t, db, e2.ID, time.Now().Add(-2*time.Hour))
	e3, _ := waitSvc.Apply(plot.ID, u3.ID, "citizen", "u3", "")
	setEntryCreatedAt(t, db, e3.ID, time.Now().Add(-1*time.Hour))

	// 认养人释放地块 -> 最早申请者受邀
	released, err := plotSvc.Release(plot.ID, owner.ID, "farmer")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if released.Status != string(constants.PlotStatusAvailable) {
		t.Fatalf("plot status=%s", released.Status)
	}
	got, err := waitRepo.FindByID(e1.ID)
	if err != nil {
		t.Fatalf("find e1: %v", err)
	}
	if got.Status != string(constants.WaitlistInvited) || got.InvitedAt == nil {
		t.Fatalf("e1 status=%s invited_at=%v, want invited", got.Status, got.InvitedAt)
	}

	// 受邀资格在认养或取消前一直有效：其他人无法认养
	if _, err := plotSvc.Adopt(plot.ID, u2.ID, "citizen", "u2"); err == nil {
		t.Fatalf("non-invitee adoption should be rejected while invite is valid")
	} else if ae, ok := err.(*util.AppError); !ok || ae.Code != constants.CodeWaitlistInviteMismatch {
		t.Fatalf("err=%v, want CodeWaitlistInviteMismatch", err)
	}

	// 受邀人完成认养 -> invited -> adopted，地块变已认养
	adopted, err := plotSvc.Adopt(plot.ID, u1.ID, "citizen", "u1")
	if err != nil {
		t.Fatalf("invitee Adopt: %v", err)
	}
	if adopted.Status != string(constants.PlotStatusAdopted) || adopted.AdopterID == nil || *adopted.AdopterID != u1.ID {
		t.Fatalf("adopt result invalid: %+v", adopted)
	}
	got, _ = waitRepo.FindByID(e1.ID)
	if got.Status != string(constants.WaitlistAdopted) {
		t.Fatalf("e1 status=%s, want adopted", got.Status)
	}
}

func TestWaitlistService_InviteCascadeOnCancel(t *testing.T) {
	db := newTestServiceDB(t)
	plotSvc, waitSvc, waitRepo := newWaitlistFixture(t, db)
	owner := newTestUser(t, db, "owner", "farmer")
	u1 := newTestUser(t, db, "c1", "citizen")
	u2 := newTestUser(t, db, "c2", "citizen")
	ownerID := owner.ID
	plot := newTestPlot(t, db, "P-WL-CASCADE", "harvested", &ownerID)

	e1, _ := waitSvc.Apply(plot.ID, u1.ID, "citizen", "c1", "")
	setEntryCreatedAt(t, db, e1.ID, time.Now().Add(-2*time.Hour))
	e2, _ := waitSvc.Apply(plot.ID, u2.ID, "citizen", "c2", "")
	setEntryCreatedAt(t, db, e2.ID, time.Now().Add(-1*time.Hour))

	// 地块仍处于已认养状态时，取消排队中的申请（waiting 终态）后可再次申请
	canceledWaiting, err := waitSvc.Cancel(e1.ID, u1.ID, "citizen")
	if err != nil {
		t.Fatalf("cancel waiting: %v", err)
	}
	if canceledWaiting.Status != string(constants.WaitlistCancelled) {
		t.Fatalf("status=%s", canceledWaiting.Status)
	}
	reapply, err := waitSvc.Apply(plot.ID, u1.ID, "citizen", "c1", "")
	if err != nil {
		t.Fatalf("re-apply after cancel: %v", err)
	}
	if reapply.Status != string(constants.WaitlistWaiting) {
		t.Fatalf("reapply status=%s", reapply.Status)
	}

	// 此时释放地块：e2 申请时间最早（u1 的重新申请更晚），e2 受邀
	if _, err := plotSvc.Release(plot.ID, owner.ID, "farmer"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	invited, err := waitRepo.FindByID(e2.ID)
	if err != nil {
		t.Fatalf("find e2: %v", err)
	}
	if invited.Status != string(constants.WaitlistInvited) {
		t.Fatalf("e2 status=%s, want invited after release", invited.Status)
	}

	// u2 受邀后取消 -> 资格顺延给仍在排队的 u1
	canceled, err := waitSvc.Cancel(e2.ID, u2.ID, "citizen")
	if err != nil {
		t.Fatalf("Cancel invited: %v", err)
	}
	if canceled.Status != string(constants.WaitlistCancelled) {
		t.Fatalf("canceled status=%s", canceled.Status)
	}
	next, err := waitRepo.FindByID(reapply.ID)
	if err != nil {
		t.Fatalf("find reapply: %v", err)
	}
	if next.Status != string(constants.WaitlistInvited) {
		t.Fatalf("u1 reapply status=%s, want invited after cascade", next.Status)
	}
}

func TestWaitlistService_CancelPermissionsAndState(t *testing.T) {
	db := newTestServiceDB(t)
	_, waitSvc, _ := newWaitlistFixture(t, db)
	owner := newTestUser(t, db, "owner2", "farmer")
	applicant := newTestUser(t, db, "applicant2", "citizen")
	other := newTestUser(t, db, "other2", "citizen")
	admin := newTestUser(t, db, "admin2", "admin")
	ownerID := owner.ID
	plot := newTestPlot(t, db, "P-WL-CANCEL", "adopted", &ownerID)

	entry, err := waitSvc.Apply(plot.ID, applicant.ID, "citizen", "applicant2", "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// 其他市民无权取消（越权拒绝）
	if _, err := waitSvc.Cancel(entry.ID, other.ID, "citizen"); err == nil {
		t.Fatalf("other user cancel should be forbidden")
	} else if ae, ok := err.(*util.AppError); !ok || ae.HTTPStatus != 403 {
		t.Fatalf("err=%v, want 403", err)
	}

	// 管理员可代为取消
	canceled, err := waitSvc.Cancel(entry.ID, admin.ID, "admin")
	if err != nil {
		t.Fatalf("admin cancel: %v", err)
	}
	if canceled.Status != string(constants.WaitlistCancelled) {
		t.Fatalf("status=%s", canceled.Status)
	}

	// 终态申请再次取消 -> 无效状态操作拒绝
	if _, err := waitSvc.Cancel(entry.ID, applicant.ID, "citizen"); err == nil {
		t.Fatalf("cancel terminal entry should be rejected")
	} else if ae, ok := err.(*util.AppError); !ok || ae.Code != constants.CodeWaitlistStateNotAllowed {
		t.Fatalf("err=%v, want CodeWaitlistStateNotAllowed", err)
	}

	// 不存在的申请 -> 404
	if _, err := waitSvc.Cancel(99999, applicant.ID, "citizen"); err == nil {
		t.Fatalf("cancel missing entry should fail")
	}
}

func TestWaitlistService_ListByPlotOrdered(t *testing.T) {
	db := newTestServiceDB(t)
	_, waitSvc, _ := newWaitlistFixture(t, db)
	owner := newTestUser(t, db, "owner3", "farmer")
	outsider := newTestUser(t, db, "outsider3", "citizen")
	u1 := newTestUser(t, db, "l1", "citizen")
	u2 := newTestUser(t, db, "l2", "citizen")
	ownerID := owner.ID
	plot := newTestPlot(t, db, "P-WL-LIST", "adopted", &ownerID)

	// 非管理员且非认养人 -> 越权查看拒绝
	if _, _, err := waitSvc.ListByPlot(plot.ID, outsider.ID, "citizen"); err == nil {
		t.Fatalf("outsider list should be forbidden")
	}

	e1, _ := waitSvc.Apply(plot.ID, u1.ID, "citizen", "l1", "")
	setEntryCreatedAt(t, db, e1.ID, time.Now().Add(-2*time.Hour))
	e2, _ := waitSvc.Apply(plot.ID, u2.ID, "citizen", "l2", "")
	setEntryCreatedAt(t, db, e2.ID, time.Now().Add(-1*time.Hour))

	// 认养人可查看，按申请时间升序
	list, total, err := waitSvc.ListByPlot(plot.ID, owner.ID, "farmer")
	if err != nil {
		t.Fatalf("ListByPlot: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("total=%d len=%d, want 2", total, len(list))
	}
	if list[0].ID != e1.ID || list[1].ID != e2.ID {
		t.Fatalf("list order = %d,%d, want %d,%d", list[0].ID, list[1].ID, e1.ID, e2.ID)
	}
	if list[0].Rank != 1 || list[1].Rank != 2 {
		t.Fatalf("ranks = %d,%d, want 1,2", list[0].Rank, list[1].Rank)
	}

	// 管理员可查看任意地块名单
	if _, _, err := waitSvc.ListByPlot(plot.ID, 999001, "admin"); err != nil {
		t.Fatalf("admin list: %v", err)
	}
}

func TestWaitlistService_Summary(t *testing.T) {
	db := newTestServiceDB(t)
	_, waitSvc, _ := newWaitlistFixture(t, db)
	owner := newTestUser(t, db, "owner4", "farmer")
	u1 := newTestUser(t, db, "s1", "citizen")
	u2 := newTestUser(t, db, "s2", "citizen")
	ownerID := owner.ID
	plot := newTestPlot(t, db, "P-WL-SUM", "adopted", &ownerID)

	if _, err := waitSvc.Apply(plot.ID, u1.ID, "citizen", "s1", ""); err != nil {
		t.Fatalf("apply u1: %v", err)
	}
	if _, err := waitSvc.Apply(plot.ID, u2.ID, "citizen", "s2", ""); err != nil {
		t.Fatalf("apply u2: %v", err)
	}

	sum, err := waitSvc.Summary(plot.ID, u2.ID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if sum.WaitingCount != 2 || sum.ActiveCount != 2 {
		t.Fatalf("counts waiting=%d active=%d, want 2/2", sum.WaitingCount, sum.ActiveCount)
	}
	if sum.Mine == nil || sum.Mine.Status != string(constants.WaitlistWaiting) || sum.Mine.Rank != 2 {
		t.Fatalf("mine=%+v, want waiting rank=2", sum.Mine)
	}

	// 匿名访问只有计数
	anon, err := waitSvc.Summary(plot.ID, 0)
	if err != nil {
		t.Fatalf("anon summary: %v", err)
	}
	if anon.WaitingCount != 2 || anon.Mine != nil {
		t.Fatalf("anon summary=%+v", anon)
	}
}
