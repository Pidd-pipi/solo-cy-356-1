package router

import (
	"github.com/gin-gonic/gin"

	"github.com/communitygarden/server/internal/middleware"
)

// registerWaitlist 候补认养路由。
//
//	POST   /plots/:id/waitlist          申请候补（登录市民）
//	GET    /plots/:id/waitlist          候补名单（管理员/认养人，service 层鉴权）
//	GET    /plots/:id/waitlist/summary  候补人数与我的申请状态（登录）
//	GET    /waitlist/mine               我的候补申请（登录）
//	GET    /waitlist/status             批量查询地块候补状态（登录）
//	DELETE /waitlist/:id                取消自己的候补申请（登录）
func (r *Router) registerWaitlist(g *gin.RouterGroup) {
	auth := g.Group("")
	auth.Use(middleware.Auth(r.cfg, r.logger))

	plots := auth.Group("/plots")
	{
		plots.POST("/:id/waitlist", r.waitlistHandler.Apply)
		plots.GET("/:id/waitlist", r.waitlistHandler.ListByPlot)
		plots.GET("/:id/waitlist/summary", r.waitlistHandler.Summary)
	}

	wl := auth.Group("/waitlist")
	{
		wl.GET("/mine", r.waitlistHandler.ListMine)
		wl.GET("/status", r.waitlistHandler.BatchStatus)
		wl.DELETE("/:id", r.waitlistHandler.Cancel)
	}
}
