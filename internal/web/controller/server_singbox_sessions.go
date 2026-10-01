package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	webruntime "github.com/SawaMEN/3x-ui/v3/internal/web/runtime"

	"github.com/gin-gonic/gin"
)

type singBoxDisconnectSessionsRequest struct {
	NodeID  *int   `json:"nodeId" form:"nodeId"`
	Inbound string `json:"inbound" form:"inbound"`
	User    string `json:"user" form:"user"`
}

// The same endpoints are also the transport used by runtime.Remote. Node-sync
// tokens must therefore be able to call the node-local form (without nodeId).
func init() {
	nodeSyncScopeAllow["/server/singbox/sessions"] = map[string]struct{}{http.MethodGet: {}}
	nodeSyncScopeAllow["/server/singbox/sessions/disconnect-user"] = map[string]struct{}{http.MethodPost: {}}
	nodeSyncScopeAllow["/server/singbox/sessions/disconnect-inbound"] = map[string]struct{}{http.MethodPost: {}}
}

func (a *ServerController) initSingBoxSessionRouter(g *gin.RouterGroup) {
	g.GET("/singbox/sessions", a.getSingBoxSessions)
	g.POST("/singbox/sessions/disconnect-user", a.disconnectSingBoxUserSessions)
	g.POST("/singbox/sessions/disconnect-inbound", a.disconnectSingBoxInboundSessions)
}

func normalizeSingBoxSessionNodeID(nodeID *int) (*int, error) {
	if nodeID == nil || *nodeID == 0 {
		return nil, nil
	}
	if *nodeID < 0 {
		return nil, errors.New("nodeId must be a positive integer")
	}
	return nodeID, nil
}

func singBoxSessionNodeIDFromQuery(c *gin.Context) (*int, error) {
	raw := strings.TrimSpace(c.Query("nodeId"))
	if raw == "" {
		return nil, nil
	}
	id, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid nodeId: %w", err)
	}
	return normalizeSingBoxSessionNodeID(&id)
}

func singBoxSessionRuntime(nodeID *int) (webruntime.SessionRuntime, error) {
	manager := webruntime.GetManager()
	if manager == nil {
		return nil, errors.New("runtime manager is not initialized")
	}
	return manager.SessionRuntimeFor(nodeID)
}

func (a *ServerController) getSingBoxSessions(c *gin.Context) {
	nodeID, err := singBoxSessionNodeIDFromQuery(c)
	if err != nil {
		jsonMsg(c, "get sing-box sessions", err)
		return
	}
	if nodeID == nil {
		sessions, err := a.singBoxService.ActiveSessions(c.Request.Context())
		jsonObj(c, sessions, err)
		return
	}

	runtime, err := singBoxSessionRuntime(nodeID)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	sessions, err := runtime.ActiveSessions(c.Request.Context())
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
	nodeID, err := normalizeSingBoxSessionNodeID(request.NodeID)
	if err != nil {
		jsonMsg(c, "disconnect sing-box user sessions", err)
		return
	}
	if nodeID == nil {
		closed, err := a.singBoxService.DisconnectUserSessions(c.Request.Context(), request.Inbound, request.User)
		jsonObj(c, gin.H{"closed": closed}, err)
		return
	}

	runtime, err := singBoxSessionRuntime(nodeID)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	closed, err := runtime.DisconnectUserSessions(c.Request.Context(), request.Inbound, request.User)
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
	nodeID, err := normalizeSingBoxSessionNodeID(request.NodeID)
	if err != nil {
		jsonMsg(c, "disconnect sing-box inbound sessions", err)
		return
	}
	if nodeID == nil {
		closed, err := a.singBoxService.DisconnectInboundSessions(c.Request.Context(), request.Inbound)
		jsonObj(c, gin.H{"closed": closed}, err)
		return
	}

	runtime, err := singBoxSessionRuntime(nodeID)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	closed, err := runtime.DisconnectInboundSessions(c.Request.Context(), request.Inbound)
	jsonObj(c, gin.H{"closed": closed}, err)
}
