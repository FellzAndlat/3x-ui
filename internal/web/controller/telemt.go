package controller

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

type TelemtController struct {
	service        service.TelemtService
	settingService service.SettingService
}

func NewTelemtController(g *gin.RouterGroup, settingService service.SettingService) *TelemtController {
	service.StartTelemtConnectionMonitor()
	a := &TelemtController{settingService: settingService}
	g.GET("/status", a.status)
	g.GET("/webproxy/status", a.webProxyStatus)
	g.POST("/webproxy/enable", a.enableWebProxy)
	g.POST("/webproxy/disable", a.disableWebProxy)
	g.GET("/config", a.config)
	g.POST("/config", a.saveConfig)
	g.GET("/subscription-proxy", a.subscriptionProxySetting)
	g.POST("/subscription-proxy", a.saveSubscriptionProxySetting)
	g.GET("/proxy", a.listProxy)
	g.POST("/proxy", a.createProxy)
	g.DELETE("/proxy/:name", a.deleteProxy)
	g.GET("/connections", a.connections)
	g.POST("/action", a.action)
	return a
}

func (a *TelemtController) status(c *gin.Context) {
	status := a.service.Status()
	status.WebProxy = a.getWebProxyStatus(c)
	jsonObj(c, status, nil)
}

func (a *TelemtController) getWebProxyStatus(c *gin.Context) service.TelemtWebProxyStatus {
	defaultDomain, _ := a.settingService.GetWebDomain()
	if strings.TrimSpace(defaultDomain) == "" {
		defaultDomain = publicHostFromRequest(c)
	}
	certFile, _ := a.settingService.GetCertFile()
	keyFile, _ := a.settingService.GetKeyFile()
	webStatus, err := a.service.WebProxyStatus(defaultDomain, certFile, keyFile)
	if err != nil {
		webStatus.Error = err.Error()
	}
	appendWebProxyError := func(message string) {
		if webStatus.Error == "" {
			webStatus.Error = message
		} else {
			webStatus.Error += "; " + message
		}
	}
	if webStatus.Enabled && !a.service.WebProxyTelemtActive() {
		appendWebProxyError("Telemt остановлен: WEB Proxy настроен, но локальный backend не работает")
	}
	if webStatus.Enabled && !webStatus.NginxActive {
		appendWebProxyError("Nginx остановлен: HTTPS endpoint WEB Proxy недоступен")
	}
	if webStatus.Enabled && !webStatus.CertificateReady {
		appendWebProxyError("TLS-сертификат WEB Proxy отсутствует, просрочен или не подходит домену")
	}
	return webStatus
}

func (a *TelemtController) webProxyStatus(c *gin.Context) {
	jsonObj(c, a.getWebProxyStatus(c), nil)
}

func (a *TelemtController) enableWebProxy(c *gin.Context) {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, "invalid WEB Proxy request", err)
		return
	}
	if strings.TrimSpace(req.Domain) == "" {
		req.Domain = a.getWebProxyStatus(c).DefaultDomain
	}
	snapshot, err := a.service.CaptureWebProxyEnableSnapshot()
	if err != nil {
		jsonMsg(c, "failed to capture WEB Proxy state", err)
		return
	}
	nginxWasInstalled := a.service.WebProxyNginxInstalled()
	certFile, _ := a.settingService.GetCertFile()
	keyFile, _ := a.settingService.GetKeyFile()
	_, err = a.service.EnableWebProxy(c.Request.Context(), req.Domain, certFile, keyFile)
	if err != nil {
		rollbackErr := a.service.RestoreFailedWebProxyEnable(snapshot)
		if !nginxWasInstalled {
			a.service.CleanupFailedWebProxyEnable()
		}
		msg := err.Error()
		if rollbackErr != nil {
			msg += "; WEB Proxy rollback failed: " + rollbackErr.Error()
		}
		jsonMsg(c, "WEB Proxy setup failed", errors.New(msg))
		return
	}
	if err := a.service.EnsureWebProxyBackend(); err != nil {
		rollbackErr := a.service.RestoreWebProxyEnableSnapshot(snapshot)
		msg := err.Error()
		if rollbackErr != nil {
			msg += "; WEB Proxy rollback failed: " + rollbackErr.Error()
		}
		jsonMsg(c, "WEB Proxy setup failed", errors.New(msg))
		return
	}
	status := a.getWebProxyStatus(c)
	status.DefaultDomain = req.Domain
	jsonObj(c, status, nil)
}

func (a *TelemtController) disableWebProxy(c *gin.Context) {
	if err := a.service.DisableWebProxy(); err != nil {
		jsonMsg(c, "failed to disable WEB Proxy", err)
		return
	}
	if err := a.service.CleanupWebProxyBackendDependency(); err != nil {
		jsonMsg(c, "WEB Proxy disabled, but failed to remove boot dependency", err)
		return
	}
	jsonObj(c, a.getWebProxyStatus(c), nil)
}

func (a *TelemtController) config(c *gin.Context) {
	cfg, err := a.service.GetConfig()
	if err != nil {
		jsonMsg(c, "failed to read Telemt config", err)
		return
	}
	jsonObj(c, cfg, nil)
}

func (a *TelemtController) saveConfig(c *gin.Context) {
	var cfg service.TelemtConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		jsonMsg(c, "invalid Telemt configuration", err)
		return
	}
	if err := a.service.SaveConfigAtomic(cfg); err != nil {
		jsonMsg(c, err.Error(), err)
		return
	}
	jsonObj(c, a.service.Status(), nil)
}

func (a *TelemtController) subscriptionProxySetting(c *gin.Context) {
	enabled, err := a.settingService.GetTelemtSubscriptionProxyEnable()
	if err != nil {
		jsonMsg(c, "failed to read Telemt subscription proxy setting", err)
		return
	}
	jsonObj(c, gin.H{"enabled": enabled}, nil)
}

func (a *TelemtController) saveSubscriptionProxySetting(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, "invalid Telemt subscription proxy setting", err)
		return
	}
	if err := a.settingService.SetTelemtSubscriptionProxyEnable(req.Enabled); err != nil {
		jsonMsg(c, "failed to save Telemt subscription proxy setting", err)
		return
	}
	jsonObj(c, gin.H{"enabled": req.Enabled}, nil)
}

func (a *TelemtController) listProxy(c *gin.Context) {
	proxies, err := a.service.ListProxies()
	if err != nil {
		jsonMsg(c, "failed to read Telemt proxies", err)
		return
	}
	host := publicHostFromRequest(c)
	if host != "" {
		for i := range proxies {
			if u, err := url.Parse(proxies[i].Link); err == nil {
				q := u.Query()
				q.Set("server", host)
				u.RawQuery = q.Encode()
				proxies[i].Link = u.String()
				proxies[i].Host = host
			}
		}
	}
	jsonObj(c, proxies, nil)
}

func (a *TelemtController) connections(c *gin.Context) {
	connections, err := a.service.ConnectedClients()
	if err != nil {
		jsonMsg(c, "failed to read Telemt connections", err)
		return
	}
	jsonObj(c, connections, nil)
}

func publicHostFromRequest(c *gin.Context) string {
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if i := strings.IndexByte(host, ','); i >= 0 {
		host = strings.TrimSpace(host[:i])
	}
	if host == "" {
		host = strings.TrimSpace(c.Request.Host)
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(host, "[]")
}

func (a *TelemtController) createProxy(c *gin.Context) {
	var req service.TelemtCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, "invalid Telemt proxy request", err)
		return
	}
	req.Host = publicHostFromRequest(c)
	if req.Host == "" {
		jsonMsg(c, "unable to determine public host", nil)
		return
	}
	proxy, err := a.service.CreateProxyAtomic(req)
	if err != nil {
		jsonMsg(c, err.Error(), err)
		return
	}
	jsonObj(c, proxy, nil)
}

func (a *TelemtController) deleteProxy(c *gin.Context) {
	if err := a.service.DeleteProxy(c.Param("name")); err != nil {
		jsonMsg(c, err.Error(), err)
		return
	}
	jsonObj(c, a.service.Status(), nil)
}

func (a *TelemtController) action(c *gin.Context) {
	var req struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, "invalid Telemt action", err)
		return
	}
	if err := a.service.Apply(req.Action); err != nil {
		jsonMsg(c, err.Error(), err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": a.service.Status()})
}
