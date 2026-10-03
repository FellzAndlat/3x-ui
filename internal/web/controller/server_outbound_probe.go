package controller

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/core"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"

	"github.com/gin-gonic/gin"
)

const defaultOutboundProbeURL = "https://www.gstatic.com/generate_204"

func (a *ServerController) initOutboundProbeRouter(g *gin.RouterGroup) {
	g.GET("/core/capabilities", a.getCoreCapabilities)
	g.POST("/singbox/outbound/check", a.checkSingBoxOutbound)
}

func (a *ServerController) selectedCore() (core.Type, error) {
	selected, err := a.settingService.GetCoreType()
	if err != nil {
		return "", err
	}
	coreType := core.Type(selected)
	if !coreType.Valid() {
		return "", fmt.Errorf("unsupported core type %q", selected)
	}
	return coreType, nil
}

func (a *ServerController) getCoreCapabilities(c *gin.Context) {
	coreType, err := a.selectedCore()
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	jsonObj(c, gin.H{
		"core":         coreType,
		"capabilities": coreType.Capabilities(),
	}, nil)
}

func (a *ServerController) checkSingBoxOutbound(c *gin.Context) {
	coreType, err := a.selectedCore()
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	if !coreType.Capabilities().OutboundDelayProbe {
		jsonObj(c, nil, fmt.Errorf("selected core %q does not support outbound delay probes", coreType))
		return
	}
	if !a.singBoxService.IsRunning() {
		jsonObj(c, nil, fmt.Errorf("sing-box is not running"))
		return
	}

	tag := strings.TrimSpace(c.PostForm("tag"))
	if tag == "" {
		jsonObj(c, nil, fmt.Errorf("outbound tag is required"))
		return
	}

	testURL := strings.TrimSpace(c.PostForm("url"))
	if testURL == "" {
		testURL = defaultOutboundProbeURL
	}

	timeout := 5 * time.Second
	if raw := strings.TrimSpace(c.PostForm("timeout")); raw != "" {
		milliseconds, err := strconv.Atoi(raw)
		if err != nil || milliseconds < 1 || milliseconds > 30000 {
			jsonObj(c, nil, fmt.Errorf("timeout must be between 1 and 30000 milliseconds"))
			return
		}
		timeout = time.Duration(milliseconds) * time.Millisecond
	}

	result, err := singbox.NewClashStatsClient().ProxyDelay(c.Request.Context(), tag, testURL, timeout)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	jsonObj(c, gin.H{
		"tag":     tag,
		"url":     testURL,
		"delay":   result.Delay,
		"delay2":  result.Delay2,
		"timeout": timeout.Milliseconds(),
	}, nil)
}
