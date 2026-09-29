package controller

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

// FirewallController exposes host-firewall management next to the proxy-core
// controls. It is mounted under /panel/api/server/firewall and therefore uses
// the panel's normal session/API authentication and CSRF protection.
type FirewallController struct {
	firewallService service.FirewallService
}

type firewallStatusResponse struct {
	service.FirewallManagedStatus
	PingEnabled bool `json:"pingEnabled"`
}

func NewFirewallController(g *gin.RouterGroup) *FirewallController {
	a := &FirewallController{}
	a.initRouter(g.Group("/firewall"))
	a.firewallService.StartAutoSync()
	return a
}

func (a *FirewallController) initRouter(g *gin.RouterGroup) {
	g.GET("/status", a.status)
	g.POST("/enabled", a.setEnabled)
	g.POST("/auto-sync", a.setAutoSync)
	g.POST("/ping", a.setPing)
	g.POST("/sync", a.sync)
	g.POST("/rules/add", a.addRule)
	g.POST("/rules/delete", a.deleteRule)
}

func (a *FirewallController) jsonStatus(c *gin.Context, status service.FirewallManagedStatus, err error) {
	if err != nil {
		jsonObj(c, status, err)
		return
	}
	pingEnabled, err := a.firewallService.ManagedPingEnabled()
	if err != nil {
		jsonObj(c, status, err)
		return
	}
	jsonObj(c, firewallStatusResponse{FirewallManagedStatus: status, PingEnabled: pingEnabled}, nil)
}

func (a *FirewallController) status(c *gin.Context) {
	if err := a.firewallService.MigrateManagedBackendIfNeeded(c.Request.Context()); err != nil {
		jsonMsg(c, "failed to reconcile firewall backend", err)
		return
	}
	status, err := a.firewallService.GetManagedStatusSafe(c.Request.Context(), firewallSafetyPort(c))
	a.jsonStatus(c, status, err)
}

func (a *FirewallController) initializeControl(c *gin.Context) bool {
	if err := a.firewallService.MigrateManagedBackendIfNeeded(c.Request.Context()); err != nil {
		jsonMsg(c, "failed to reconcile firewall backend", err)
		return false
	}
	if err := a.firewallService.RememberSafetyPort(firewallSafetyPort(c)); err != nil {
		jsonMsg(c, "failed to persist firewall safety port", err)
		return false
	}
	if err := a.firewallService.MarkControlInitialized(); err != nil {
		jsonMsg(c, "failed to initialize firewall management", err)
		return false
	}
	return true
}

func (a *FirewallController) setEnabled(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled" form:"enabled"`
	}
	if err := c.ShouldBind(&req); err != nil {
		jsonMsg(c, "invalid firewall state", err)
		return
	}
	if !a.initializeControl(c) {
		return
	}
	status, err := a.firewallService.SetManagedEnabledSafe(c.Request.Context(), req.Enabled, firewallSafetyPort(c))
	if err == nil {
		status, err = a.firewallService.ReconcileManagedPingState(c.Request.Context(), firewallSafetyPort(c))
	}
	a.jsonStatus(c, status, err)
}

func (a *FirewallController) setAutoSync(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled" form:"enabled"`
	}
	if err := c.ShouldBind(&req); err != nil {
		jsonMsg(c, "invalid firewall auto-sync state", err)
		return
	}
	if !a.initializeControl(c) {
		return
	}
	status, err := a.firewallService.SetManagedAutoSyncPreferenceSafe(c.Request.Context(), req.Enabled, firewallSafetyPort(c))
	a.jsonStatus(c, status, err)
}

func (a *FirewallController) setPing(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled" form:"enabled"`
	}
	if err := c.ShouldBind(&req); err != nil {
		jsonMsg(c, "invalid firewall ping state", err)
		return
	}
	if !a.initializeControl(c) {
		return
	}
	status, err := a.firewallService.SetManagedPingEnabledSafe(c.Request.Context(), req.Enabled, firewallSafetyPort(c))
	a.jsonStatus(c, status, err)
}

func (a *FirewallController) sync(c *gin.Context) {
	if !a.initializeControl(c) {
		return
	}
	status, err := a.firewallService.SyncManagedSafe(c.Request.Context(), firewallSafetyPort(c))
	a.jsonStatus(c, status, err)
}

func (a *FirewallController) addRule(c *gin.Context) {
	var req struct {
		Port     int    `json:"port" form:"port"`
		Protocol string `json:"protocol" form:"protocol"`
		Label    string `json:"label" form:"label"`
	}
	if err := c.ShouldBind(&req); err != nil {
		jsonMsg(c, "invalid firewall rule", err)
		return
	}
	if !a.initializeControl(c) {
		return
	}
	status, err := a.firewallService.AddManagedManualRuleSafe(c.Request.Context(), req.Port, req.Protocol, req.Label, firewallSafetyPort(c))
	a.jsonStatus(c, status, err)
}

func (a *FirewallController) deleteRule(c *gin.Context) {
	var req struct {
		Port     int    `json:"port" form:"port"`
		Protocol string `json:"protocol" form:"protocol"`
	}
	if err := c.ShouldBind(&req); err != nil {
		jsonMsg(c, "invalid firewall rule", err)
		return
	}
	if !a.initializeControl(c) {
		return
	}
	status, err := a.firewallService.DeleteManagedManualRuleSafe(c.Request.Context(), req.Port, req.Protocol, firewallSafetyPort(c))
	a.jsonStatus(c, status, err)
}

// firewallAutoSyncMiddleware coalesces successful inbound mutation requests
// into an immediate firewall reconcile. The periodic worker remains a safety
// net for imports and future mutation paths that bypass this API group.
func firewallAutoSyncMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Request.Method != http.MethodPost {
			return
		}
		path := c.FullPath()
		if strings.Contains(path, "/panel/api/inbounds/") {
			service.TriggerFirewallSync()
		}
	}
}

// firewallSafetyPort protects the very HTTP(S) port from which the operator is
// enabling the firewall, including a trusted reverse proxy's external port.
// This supplements the persisted panel/subscription/SSH ports collected by the
// service and prevents the toggle itself from locking the current panel out.
func firewallSafetyPort(c *gin.Context) int {
	if isTrustedForwardedRequest(c) {
		if raw := strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Port"), ",")[0]); raw != "" {
			if port, err := strconv.Atoi(raw); err == nil && port >= 1 && port <= 65535 {
				return port
			}
		}
	}
	if _, raw, err := net.SplitHostPort(c.Request.Host); err == nil {
		if port, err := strconv.Atoi(raw); err == nil && port >= 1 && port <= 65535 {
			return port
		}
	}
	if isTrustedForwardedRequest(c) {
		switch strings.ToLower(strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0])) {
		case "https":
			return 443
		case "http":
			return 80
		}
	}
	if c.Request.TLS != nil {
		return 443
	}
	return 0
}
