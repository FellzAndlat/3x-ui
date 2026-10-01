package controller

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

type singBoxDisconnectSessionsRequest struct {
	Inbound string `json:"inbound" form:"inbound"`
	User    string `json:"user" form:"user"`
}

func (a *ServerController) initSingBoxSessionRouter(g *gin.RouterGroup) {
	g.GET("/singbox/sessions", a.getSingBoxSessions)
	g.POST("/singbox/sessions/disconnect-user", a.disconnectSingBoxUserSessions)
	g.POST("/singbox/sessions/disconnect-inbound", a.disconnectSingBoxInboundSessions)
}

func (a *ServerController) getSingBoxSessions(c *gin.Context) {
	sessions, err := a.singBoxService.ActiveSessions(c.Request.Context())
	jsonObj(c, sessions, err)
}

func (a *ServerController) disconnectSingBoxUserSessions(c *gin.Context) {
	var request singBoxDisconnectSessionsRequest
	if err := c.ShouldBind(&request); err != nil {
		jsonMsg(c, "disconnect sing-box user sessions", err)
		return
	}
	request.Inbound = strings.TrimSpace(request.Inbound)
	request.User = strings.TrimSpace(request.User)
	if request.User == "" {
		jsonMsg(c, "disconnect sing-box user sessions", errors.New("user is required"))
		return
	}
	closed, err := a.singBoxService.DisconnectUserSessions(c.Request.Context(), request.Inbound, request.User)
	jsonObj(c, gin.H{"closed": closed}, err)
}

func (a *ServerController) disconnectSingBoxInboundSessions(c *gin.Context) {
	var request singBoxDisconnectSessionsRequest
	if err := c.ShouldBind(&request); err != nil {
		jsonMsg(c, "disconnect sing-box inbound sessions", err)
		return
	}
	request.Inbound = strings.TrimSpace(request.Inbound)
	if request.Inbound == "" {
		jsonMsg(c, "disconnect sing-box inbound sessions", errors.New("inbound is required"))
		return
	}
	closed, err := a.singBoxService.DisconnectInboundSessions(c.Request.Context(), request.Inbound)
	jsonObj(c, gin.H{"closed": closed}, err)
}
