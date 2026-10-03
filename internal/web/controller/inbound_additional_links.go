package controller

import (
	"fmt"
	"github.com/SawaMEN/3x-ui/v3/internal/web/session"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (a *InboundController) getScopedInboundLinks(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	user := session.GetLoginUser(c)
	ib, err := a.inboundService.GetInbound(id)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	if user == nil || ib.UserId != user.Id {
		jsonObj(c, nil, fmt.Errorf("inbound is not available to this user"))
		return
	}
	links, err := a.inboundService.GetScopedInboundLinks(resolveHost(c), ib, c.Query("email"))
	jsonObj(c, links, err)
}
