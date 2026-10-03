package controller

import (
	"fmt"
	"runtime"

	"github.com/gin-gonic/gin"

	"github.com/SawaMEN/3x-ui/v3/internal/naiveproxy"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

type NaiveProxyController struct {
	settingService service.SettingService
}

func NewNaiveProxyController(g *gin.RouterGroup) *NaiveProxyController {
	a := &NaiveProxyController{}
	group := g.Group("/naiveproxy")
	group.GET("/status", a.status)
	group.POST("/update", a.update)
	naiveproxy.StartAutomaticLifecycle()
	return a
}

func (a *NaiveProxyController) standaloneAllowed() error {
	coreType, err := a.settingService.GetCoreType()
	if err != nil {
		return err
	}
	if coreType != service.CoreTypeXray {
		return fmt.Errorf("standalone NaiveProxy is available only when Xray is selected")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("official Caddy-Naive server release supports linux/amd64 only; current platform is %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return nil
}

func (a *NaiveProxyController) status(c *gin.Context) {
	if err := a.standaloneAllowed(); err != nil {
		jsonObj(c, naiveproxy.Status{}, err)
		return
	}
	status, err := naiveproxy.GetStatus(c.Request.Context())
	jsonObj(c, status, err)
}

func (a *NaiveProxyController) update(c *gin.Context) {
	if err := a.standaloneAllowed(); err != nil {
		jsonObj(c, naiveproxy.Status{}, err)
		return
	}
	status, err := naiveproxy.Update(c.Request.Context())
	if err == nil {
		if ensureErr := naiveproxy.Ensure(c.Request.Context()); ensureErr != nil {
			err = ensureErr
		} else if refreshed, statusErr := naiveproxy.GetStatus(c.Request.Context()); statusErr == nil {
			status = refreshed
		}
	}
	jsonObj(c, status, err)
}
