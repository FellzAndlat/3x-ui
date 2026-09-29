package sub

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

type telemtSubscriptionProfile struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	TLS  bool   `json:"tls"`
	Link string `json:"link"`
}

type telemtSubscriptionPayload struct {
	Personal        *telemtSubscriptionProfile `json:"personal,omitempty"`
	WebProxyEnabled bool                        `json:"webProxyEnabled"`
	WebProxy        string                      `json:"webProxy,omitempty"`
}

func registerTelemtSubscriptionRoute(g *gin.RouterGroup) {
	// Keep the endpoint next to the configured browser subscription URL so it
	// continues to work when /sub/ is customized or exposed through a reverse
	// proxy that forwards the subscription prefix.
	settings := service.SettingService{}
	subPath, err := settings.GetSubPath()
	if err != nil {
		subPath = "/sub/"
	}
	path := "/" + strings.Trim(subPath, "/") + "/telemt/:subid"
	if strings.Trim(subPath, "/") == "" {
		path = "/telemt/:subid"
	}
	g.GET(path, serveTelemtSubscription)
}

func serveTelemtSubscription(c *gin.Context) {
	subID := strings.TrimSpace(c.Param("subid"))
	if subID == "" {
		c.Status(http.StatusNotFound)
		return
	}

	resolver := NewSubService("")
	_, host, _, _ := resolver.ResolveRequest(c)
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	// Validate the subscription through the same data path used by the normal
	// raw/browser subscription instead of depending on a particular DB schema.
	// This also rejects deleted or never-existing subIds consistently.
	subReq := resolver.ForRequest(host)
	subReq.subscriptionBody = false
	subs, _, _, _, err := subReq.getSubs(subID)
	if err != nil || subs == nil {
		writeSubError(c, err)
		return
	}

	telemt := service.TelemtService{}
	payload := telemtSubscriptionPayload{}
	if personal, err := telemt.EnsureSubscriptionProxy(subID, host); err == nil {
		payload.Personal = &telemtSubscriptionProfile{
			Host: personal.Host,
			Port: personal.Port,
			TLS:  personal.TLS,
			Link: personal.Link,
		}
	} else {
		logger.Debug("sub: Telemt personal profile unavailable:", err)
	}

	settings := service.SettingService{}
	defaultDomain, _ := settings.GetWebDomain()
	if strings.TrimSpace(defaultDomain) == "" {
		defaultDomain = host
	}
	certFile, _ := settings.GetCertFile()
	keyFile, _ := settings.GetKeyFile()
	if web, err := telemt.WebProxyStatus(defaultDomain, certFile, keyFile); err == nil {
		payload.WebProxyEnabled = web.Enabled
		if web.Enabled && strings.TrimSpace(web.Link) != "" {
			payload.WebProxy = web.Link
		}
	}

	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, payload)
}
