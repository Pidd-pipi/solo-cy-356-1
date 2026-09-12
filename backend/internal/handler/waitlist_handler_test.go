package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/communitygarden/server/internal/config"
	"github.com/communitygarden/server/internal/constants"
	"github.com/communitygarden/server/internal/middleware"
	"github.com/communitygarden/server/internal/model"
	"github.com/communitygarden/server/internal/repository"
	"github.com/communitygarden/server/internal/service"
	"github.com/communitygarden/server/internal/util"
)

// noopAudit 测试用空审计写入器。
type noopAudit struct{}

func (noopAudit) Write(uint, string, string, string, string, string, string, string, string) error {
	return nil
}

type waitlistAPIFixture struct {
	engine  *gin.Engine
	db      *gorm.DB
	tokens  map[uint]string
	plotID  uint
	ownerID uint
	users   map[string]*model.User
}

func setupWaitlistAPI(t *testing.T) *waitlistAPIFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:api_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Plot{}, &model.WaitlistEntry{}, &model.PlantingPlan{}, &model.HarvestRecord{},
		&model.DiaryEntry{}, &model.DiaryComment{}, &model.CommunityPost{}, &model.CommunityComment{}, &model.AuditLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS waitlist_active_uniq
		ON waitlist_entries (plot_id, user_id) WHERE status IN ('waiting','invited')`).Error; err != nil {
		t.Fatalf("partial index: %v", err)
	}

	mkUser := func(username, role string) *model.User {
		u := &model.User{Username: username, Password: "x", Nickname: username, Role: role, Status: "active"}
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("create user: %v", err)
		}
		return u
	}
	admin := mkUser("api_admin", "admin")
	owner := mkUser("api_owner", "farmer")
	applicant := mkUser("api_applicant", "citizen")
	other := mkUser("api_other", "citizen")

	plot := &model.Plot{Name: "API 地块", Code: "API-P1", Area: 9, SoilType: "loam",
		Sunlight: "full", Latitude: 31, Longitude: 121, Status: string(constants.PlotStatusHarvested), AdopterID: &owner.ID}
	if err := db.Create(plot).Error; err != nil {
		t.Fatalf("create plot: %v", err)
	}

	plotRepo := repository.NewPlotRepository(db)
	waitRepo := repository.NewWaitlistRepository(db)
	plotSvc := service.NewPlotService(plotRepo, db, util.NewLogger("error"))
	waitSvc := service.NewWaitlistService(waitRepo, plotRepo, db, util.NewLogger("error"))
	plotSvc.SetWaitlistHooks(waitSvc, waitSvc)
	plotHandler := NewPlotHandler(plotSvc, noopAudit{})
	waitHandler := NewWaitlistHandler(waitSvc, noopAudit{})

	cfg := &config.Config{JWTSecret: "test-secret", JWTExpireHours: 1}
	engine := gin.New()
	v1 := engine.Group("/api/v1")
	auth := v1.Group("")
	auth.Use(middleware.Auth(cfg, util.NewLogger("error")))
	auth.POST("/plots/:id/adopt", plotHandler.Adopt)
	auth.POST("/plots/:id/release", plotHandler.Release)
	auth.POST("/plots/:id/waitlist", waitHandler.Apply)
	auth.GET("/plots/:id/waitlist", waitHandler.ListByPlot)
	auth.GET("/plots/:id/waitlist/summary", waitHandler.Summary)
	auth.GET("/waitlist/mine", waitHandler.ListMine)
	auth.GET("/waitlist/status", waitHandler.BatchStatus)
	auth.DELETE("/waitlist/:id", waitHandler.Cancel)

	token := func(uid uint, username, role string) string {
		tk, err := util.GenerateToken(cfg.JWTSecret, 1, uid, username, role)
		if err != nil {
			t.Fatalf("token: %v", err)
		}
		return tk
	}
	return &waitlistAPIFixture{
		engine: engine, db: db, plotID: plot.ID, ownerID: owner.ID,
		users: map[string]*model.User{"admin": admin, "owner": owner, "applicant": applicant, "other": other},
		tokens: map[uint]string{
			admin.ID:     token(admin.ID, "api_admin", "admin"),
			owner.ID:     token(owner.ID, "api_owner", "farmer"),
			applicant.ID: token(applicant.ID, "api_applicant", "citizen"),
			other.ID:     token(other.ID, "api_other", "citizen"),
		},
	}
}

func (f *waitlistAPIFixture) do(t *testing.T, method, path, token string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, req)
	var out map[string]interface{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func TestWaitlistAPI_FullFlow(t *testing.T) {
	f := setupWaitlistAPI(t)
	appToken := f.tokens[f.users["applicant"].ID]
	otherToken := f.tokens[f.users["other"].ID]
	ownerToken := f.tokens[f.users["owner"].ID]
	adminToken := f.tokens[f.users["admin"].ID]

	// 1. 未登录拒绝
	if code, _ := f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/waitlist", f.plotID), "", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous apply status=%d, want 401", code)
	}

	// 2. 申请候补成功
	code, body := f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/waitlist", f.plotID), appToken, map[string]string{"note": "草莓"})
	if code != http.StatusOK {
		t.Fatalf("apply status=%d body=%v", code, body)
	}
	data := body["data"].(map[string]interface{})
	entryID := uint(data["id"].(float64))
	if data["status"] != "waiting" {
		t.Fatalf("entry status=%v", data["status"])
	}

	// 3. 重复申请被拒绝（409 + 2011）
	code, body = f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/waitlist", f.plotID), appToken, nil)
	if code != http.StatusConflict {
		t.Fatalf("duplicate apply status=%d, want 409", code)
	}
	if int(body["code"].(float64)) != constants.CodeWaitlistDuplicate {
		t.Fatalf("duplicate code=%v, want %d", body["code"], constants.CodeWaitlistDuplicate)
	}

	// 4. 越权查看候补名单：既非管理员也非认养人 -> 403
	if code, body = f.do(t, "GET", fmt.Sprintf("/api/v1/plots/%d/waitlist", f.plotID), otherToken, nil); code != http.StatusForbidden {
		t.Fatalf("other list status=%d body=%v, want 403", code, body)
	}

	// 5. 越权取消他人申请 -> 403
	if code, _ = f.do(t, "DELETE", fmt.Sprintf("/api/v1/waitlist/%d", entryID), otherToken, nil); code != http.StatusForbidden {
		t.Fatalf("other cancel status=%d, want 403", code)
	}

	// 6. 认养人可查看名单，按申请时间排序
	code, body = f.do(t, "GET", fmt.Sprintf("/api/v1/plots/%d/waitlist", f.plotID), ownerToken, nil)
	if code != http.StatusOK {
		t.Fatalf("owner list status=%d", code)
	}
	listData := body["data"].(map[string]interface{})
	if listData["total"].(float64) != 1 {
		t.Fatalf("list total=%v, want 1", listData["total"])
	}

	// 7. 第二人申请，释放地块 -> 最早申请者受邀，非受邀人不能认养
	_, _ = f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/waitlist", f.plotID), otherToken, nil)
	if code, body = f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/release", f.plotID), ownerToken, nil); code != http.StatusOK {
		t.Fatalf("release status=%d body=%v", code, body)
	}
	// 地块处于受邀预占：后来者 other 认养被拒
	if code, body = f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/adopt", f.plotID), otherToken, nil); code != http.StatusConflict {
		t.Fatalf("non-invitee adopt status=%d body=%v, want 409", code, body)
	}
	// 最早申请者 applicant 完成认养
	if code, body = f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/adopt", f.plotID), appToken, nil); code != http.StatusOK {
		t.Fatalf("invitee adopt status=%d body=%v", code, body)
	}

	// 8. 无效状态操作：申请已随认养变为 adopted，再次取消 -> 409 + 2013
	code, body = f.do(t, "DELETE", fmt.Sprintf("/api/v1/waitlist/%d", entryID), appToken, nil)
	if code != http.StatusConflict {
		t.Fatalf("cancel adopted entry status=%d, want 409", code)
	}
	if int(body["code"].(float64)) != constants.CodeWaitlistStateNotAllowed {
		t.Fatalf("cancel adopted code=%v, want %d", body["code"], constants.CodeWaitlistStateNotAllowed)
	}

	// 9. 管理员视角 summary 与批量状态接口可用
	if code, _ = f.do(t, "GET", fmt.Sprintf("/api/v1/plots/%d/waitlist/summary", f.plotID), adminToken, nil); code != http.StatusOK {
		t.Fatalf("summary status=%d", code)
	}
	if code, body = f.do(t, "GET", fmt.Sprintf("/api/v1/waitlist/status?plot_ids=%d", f.plotID), adminToken, nil); code != http.StatusOK {
		t.Fatalf("batch status=%d body=%v", code, body)
	}
	if code, _ = f.do(t, "GET", "/api/v1/waitlist/mine?page=1&page_size=10", appToken, nil); code != http.StatusOK {
		t.Fatalf("mine status=%d", code)
	}
}

func TestWaitlistAPI_CancelInviteCascades(t *testing.T) {
	f := setupWaitlistAPI(t)
	u1 := f.tokens[f.users["applicant"].ID]
	u2 := f.tokens[f.users["other"].ID]
	owner := f.tokens[f.users["owner"].ID]

	_, body := f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/waitlist", f.plotID), u1, nil)
	id1 := uint(body["data"].(map[string]interface{})["id"].(float64))
	_, _ = f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/waitlist", f.plotID), u2, nil)

	// 释放 -> applicant 受邀
	if _, body := f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/release", f.plotID), owner, nil); body["code"].(float64) != 0 {
		t.Fatalf("release failed: %v", body)
	}
	// applicant 取消受邀资格
	if code, _ := f.do(t, "DELETE", fmt.Sprintf("/api/v1/waitlist/%d", id1), u1, nil); code != http.StatusOK {
		t.Fatalf("cancel invited status=%d", code)
	}
	// 资格顺延给 other：applicant 此时不能认养，other 可以
	if code, _ := f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/adopt", f.plotID), u1, nil); code != http.StatusConflict {
		t.Fatalf("u1 adopt after cancelling invite status=%d, want 409", code)
	}
	if code, _ := f.do(t, "POST", fmt.Sprintf("/api/v1/plots/%d/adopt", f.plotID), u2, nil); code != http.StatusOK {
		t.Fatalf("u2 adopt after cascade status=%d, want 200", code)
	}
}
