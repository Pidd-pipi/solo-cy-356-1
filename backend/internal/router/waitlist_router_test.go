package router

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestWaitlistRoutesNoConflict 确保候补路由（含 /waitlist/:id 与静态 /waitlist/mine）
// 与既有地块路由在同一 Gin 路由树中注册不会触发 panic。
func TestWaitlistRoutesNoConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	v1 := engine.Group("/api/v1")

	noop := func(c *gin.Context) { c.JSON(200, gin.H{"code": 0}) }
	// 模拟 registerPlots 中的 plots 路由骨架（路径必须与 router/plot.go 一致）
	plots := v1.Group("/plots")
	plots.GET("", noop)
	plots.GET("/:id", noop)
	plots.POST("/:id/adopt", noop)
	plots.POST("/:id/release", noop)

	wl := v1.Group("/waitlist")
	wl.GET("/mine", noop)
	wl.GET("/status", noop)
	wl.DELETE("/:id", noop)
	plots.POST("/:id/waitlist", noop)
	plots.GET("/:id/waitlist", noop)
	plots.GET("/:id/waitlist/summary", noop)

	// 列出路由，确认关键路径均已注册
	paths := map[string]bool{}
	for _, ri := range engine.Routes() {
		paths[ri.Method+" "+ri.Path] = true
	}
	want := []string{
		"POST /api/v1/plots/:id/waitlist",
		"GET /api/v1/plots/:id/waitlist",
		"GET /api/v1/plots/:id/waitlist/summary",
		"GET /api/v1/waitlist/mine",
		"GET /api/v1/waitlist/status",
		"DELETE /api/v1/waitlist/:id",
	}
	for _, p := range want {
		if !paths[p] {
			t.Errorf("route %s not registered", p)
		}
	}
}
