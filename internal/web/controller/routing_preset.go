package controller

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
	"github.com/SawaMEN/3x-ui/v3/internal/web/session"
)

type RoutingPresetController struct {
	service service.RoutingPresetService
}

func NewRoutingPresetController(g *gin.RouterGroup) *RoutingPresetController {
	a := &RoutingPresetController{}
	g.GET("/list", a.list)
	g.GET("/get/:id", a.get)
	g.POST("/save", a.save)
	g.POST("/del/:id", a.del)
	return a
}

func (a *RoutingPresetController) userID(c *gin.Context) int {
	u := session.GetLoginUser(c)
	if u == nil {
		return 0
	}
	return u.Id
}

func (a *RoutingPresetController) list(c *gin.Context) {
	items, err := a.service.List(a.userID(c))
	jsonObj(c, items, err)
}

func (a *RoutingPresetController) get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		jsonMsg(c, "invalid preset id", errors.New("id must be a positive integer"))
		return
	}
	item, err := a.service.Get(a.userID(c), id)
	jsonObj(c, item, err)
}

func (a *RoutingPresetController) save(c *gin.Context) {
	var in service.RoutingPresetInput
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, "invalid routing preset payload", err)
		return
	}
	item, err := a.service.Save(a.userID(c), in)
	jsonObj(c, item, err)
}

func (a *RoutingPresetController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		jsonMsg(c, "invalid preset id", errors.New("id must be a positive integer"))
		return
	}
	err = a.service.Delete(a.userID(c), id)
	jsonMsg(c, "routing preset deleted", err)
}
