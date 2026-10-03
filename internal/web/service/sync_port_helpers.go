package service

import "github.com/SawaMEN/3x-ui/v3/internal/database/model"

var loopbackBind = "127.0.0.1"

func inboundBindAddr(ib *model.Inbound) string {
	if ib == nil {
		return ""
	}
	return ib.Listen
}
