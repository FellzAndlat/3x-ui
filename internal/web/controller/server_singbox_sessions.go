package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	webruntime "github.com/SawaMEN/3x-ui/v3/internal/web/runtime"

	"github.com/gin-gonic/gin"
)

type singBoxDisconnectSessionsRequest struct {
	NodeID  *int     `json:"nodeId" form:"nodeId"`
	Inbound string   `json:"inbound" form:"inbound"`
	User    string   `json:"user" form:"user"`
	Users   []string `json:"users" form:"users"`
}

// The same endpoints are also the transport used by runtime.Remote. Node-sync
// tokens may call only the node-local form (without nodeId); otherwise a node
// could use its transport credential to relay session commands through another
// panel runtime.
func init() {
	nodeSyncScopeAllow["/server/singbox/sessions"] = map[string]struct{}{http.MethodGet: {}}
	nodeSyncScopeAllow["/server/singbox/sessions/disconnect-user"] = map[string]struct{}{http.MethodPost: {}}
	nodeSyncScopeAllow["/server/singbox/sessions/disconnect-users"] = map[string]struct{}{http.MethodPost: {}}
	nodeSyncScopeAllow["/server/singbox/sessions/disconnect-inbound"] = map[string]struct{}{http.MethodPost: {}}
}

func (a *ServerController) initSingBoxSessionRouter(g *gin.RouterGroup) {
	g.GET("/singbox/sessions", a.getSingBoxSessions)
	g.POST("/singbox/sessions/disconnect-user", a.disconnectSingBoxUserSessions)
	g.POST("/singbox/sessions/disconnect-users", a.disconnectSingBoxUsersSessions)
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

func normalizeSingBoxSessionNodeTarget(c *gin.Context, nodeID *int) (*int, error) {
	nodeID, err := normalizeSingBoxSessionNodeID(nodeID)
	if err != nil || nodeID == nil {
		return nodeID, err
	}
	if scope, ok := c.Get("api_token_scope"); ok && scope == model.ApiScopeNodeSync {
		return nil, errors.New("node-sync session requests may target only the local runtime")
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
	return normalizeSingBoxSessionNodeTarget(c, &id)
}

func singBoxSessionRuntime(nodeID *int) (webruntime.SessionRuntime, error) {
	manager := webruntime.GetManager()
	if manager == nil {
		return nil, errors.New("runtime manager is not initialized")
	}
	return manager.SessionRuntimeFor(nodeID)
}

func normalizeSingBoxSessionUsers(users []string) []string {
	seen := make(map[string]struct{}, len(users))
	normalized := make([]string, 0, len(users))
	for _, user := range users {
		user = strings.TrimSpace(user)
		if user == "" {
			continue
		}
		if _, exists := seen[user]; exists {
			continue
		}
		seen[user] = struct{}{}
		normalized = append(normalized, user)
	}
	return normalized
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
	request.User = strings.TrimSpace(request.User)
	if request.User == "" {
		jsonMsg(c, "disconnect sing-box user sessions", errors.New("user is required"))
		return
	}
	request.Users = []string{request.User}
	a.disconnectSingBoxUsersSessionsRequest(c, &request)
}

func (a *ServerController) disconnectSingBoxUsersSessions(c *gin.Context) {
	var request singBoxDisconnectSessionsRequest
	if err := c.ShouldBind(&request); err != nil {
		jsonMsg(c, "disconnect sing-box user sessions", err)
		return
	}
	request.Users = normalizeSingBoxSessionUsers(request.Users)
	if len(request.Users) == 0 {
		jsonMsg(c, "disconnect sing-box user sessions", errors.New("at least one user is required"))
		return
	}
	a.disconnectSingBoxUsersSessionsRequest(c, &request)
}

func (a *ServerController) disconnectSingBoxUsersSessionsRequest(c *gin.Context, request *singBoxDisconnectSessionsRequest) {
	request.Inbound = strings.TrimSpace(request.Inbound)
	request.Users = normalizeSingBoxSessionUsers(request.Users)
	nodeID, err := normalizeSingBoxSessionNodeTarget(c, request.NodeID)
	if err != nil {
		jsonMsg(c, "disconnect sing-box user sessions", err)
		return
	}
	if nodeID == nil {
		closed, err := a.singBoxService.DisconnectUsersSessions(c.Request.Context(), request.Inbound, request.Users)
		jsonObj(c, gin.H{"closed": closed}, err)
		return
	}

	runtime, err := singBoxSessionRuntime(nodeID)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	closed, err := runtime.DisconnectUsersSessions(c.Request.Context(), request.Inbound, request.Users)
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
	nodeID, err := normalizeSingBoxSessionNodeTarget(c, request.NodeID)
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
