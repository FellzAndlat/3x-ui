package sub

import (
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestSubscriptionBatchesHostQueries(t *testing.T) {
	seedSubDB(t)
	for i := range 5 {
		ib := seedSubInbound(t, "batch", fmt.Sprintf("in-%d", i), 4400+i, i, wsTLSStream)
		seedHost(t, &model.Host{InboundId: ib.Id, Address: fmt.Sprintf("cdn-%d.example", i), Port: 443})
	}
	queries := 0
	cb := database.GetDB().Callback().Query()
	if err := cb.Before("gorm:query").Register("test:count-host-queries", func(tx *gorm.DB) {
		if tx.Statement.Table == "hosts" {
			queries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cb.Remove("test:count-host-queries") })
	links, _, _, _, err := NewSubService("").GetSubs("batch", "panel.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 5 {
		t.Fatalf("link count = %d, want 5", len(links))
	}
	for i, link := range links {
		if !strings.Contains(link, fmt.Sprintf("cdn-%d.example:443", i)) {
			t.Fatalf("link %d lost its host override: %s", i, link)
		}
	}
	if queries != 1 {
		t.Fatalf("host queries = %d, want 1 for all five inbounds", queries)
	}
}
