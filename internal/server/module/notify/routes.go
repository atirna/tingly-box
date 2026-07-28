package notify

import "github.com/gin-gonic/gin"

// RegisterRoutes registers notification hook routes behind the given auth
// middleware. This is the Claude Code hook path — the same operator user
// token as the web UI / control-plane API (see .design/bot-interaction-api.md
// §3.1, §3.6). authMW must not be nil in production; passing gin.HandlerFunc
// wraps every scenario, so callers that genuinely want it open (e.g. a
// stock/offline setup with no user token configured yet) must say so
// explicitly by passing a no-op middleware rather than omitting one.
func RegisterRoutes(engine *gin.Engine, handler *Handler, authMW gin.HandlerFunc) {
	ccGroup := engine.Group("/tingly/:scenario", authMW)
	ccGroup.POST("/notify", handler.Notify)
	ccGroup.GET("/wait/:request_id", handler.Wait)
}
