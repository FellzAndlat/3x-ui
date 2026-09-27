package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func (a *ServerController) initHiddifyExportRouter(g *gin.RouterGroup) {
	g.GET("/hiddify/export", a.exportHiddify)
}

func (a *ServerController) exportHiddify(c *gin.Context) {
	data, stats, err := a.clientService.ExportHiddifyBackup(&a.settingService)
	if err != nil {
		jsonMsg(c, "Hiddify export failed", err)
		return
	}

	filename := fmt.Sprintf("hiddify-export-%s.json", time.Now().UTC().Format("20060102-150405"))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("X-3X-UI-Hiddify-Exported-Users", strconv.Itoa(stats.Exported))
	c.Header("X-3X-UI-Hiddify-Skipped-Users", strconv.Itoa(stats.Skipped))
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}
