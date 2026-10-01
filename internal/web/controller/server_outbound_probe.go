package controller

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/singbox"

	"github.com/gin-gonic/gin"
)

const defaultOutboundProbeURL = "https://www.gstatic.com/generate_204"

func (a *ServerController) initOutboundProbeRouter(g *gin.RouterGroup) {
	g.POST("/singbox/outbound/check", a.checkSingBoxOutbound)
}

func (a *ServerController) checkSingBoxOutbound(c *gin.Context) {
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
