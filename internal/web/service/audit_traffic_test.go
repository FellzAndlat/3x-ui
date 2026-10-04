package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
	"github.com/mattn/go-sqlite3"
)

func TestAuditResetAllReenablesEveryProjectionAndPreservesGroups(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	ib := seedLocalDisabledClient(t, svc, 55001, "", "spent@x", 1000, 0, 600, 500)
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("email = ?", "spent@x").UpdateColumn("group_name", "paid").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetGroupTraffic("paid"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResetAllTraffics(); err != nil {
		t.Fatal(err)
	}
	assertEnableEverywhere(t, svc, &InboundService{}, ib.Id, "spent@x", true)
	if err := database.GetDB().Model(&xray.ClientTraffic{}).Where("email = ?", "spent@x").Update("up", 25).Error; err != nil {
		t.Fatal(err)
	}
	if g := groupByName(t, svc, "paid"); g.Up != 25 {
		t.Fatalf("group hid new usage: %+v", g)
	}
}

type auditResetRuntime struct {
	fakeNodeRuntime
	mu     sync.Mutex
	emails []string
	fail   bool
}

func (r *auditResetRuntime) ResetClientTraffic(_ context.Context, _ *model.Inbound, email string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.emails = append(r.emails, email)
	if r.fail {
		return errors.New("node unavailable")
	}
	return nil
}

func TestAuditBulkResetReachesNodeOnceAndRetainsFailedBaseline(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			setupBulkDB(t)
			db := database.GetDB()
			mgr := useTestRuntimeManager(t)
			node := &model.Node{Name: "node", Address: "127.0.0.1", Port: 2053, ApiToken: "test", Enable: true}
			if err := db.Create(node).Error; err != nil {
				t.Fatal(err)
			}
			rt := &auditResetRuntime{fail: fail}
			mgr.SetRuntimeOverride(node.Id, rt)
			clients := []model.Client{{Email: "shared@x", ID: "11111111-1111-4111-8111-111111111111", Enable: true}}
			svc := &ClientService{}
			for _, port := range []int{55002, 55003} {
				ib := mkInbound(t, port, model.VLESS, clientsSettings(t, clients))
				if err := db.Model(ib).Update("node_id", node.Id).Error; err != nil {
					t.Fatal(err)
				}
				if err := svc.SyncInbound(nil, ib.Id, clients); err != nil {
					t.Fatal(err)
				}
			}
			mkTraffic(t, 1, "shared@x", 100, 50, 0, 0, true)
			if err := db.Create(&model.NodeClientTraffic{NodeId: node.Id, Email: "shared@x", Up: 100, Down: 50}).Error; err != nil {
				t.Fatal(err)
			}
			affected, err := svc.BulkResetTraffic(&InboundService{}, []string{"shared@x"})
			if affected != 1 || (err != nil) != fail {
				t.Fatalf("reset result = %d, %v", affected, err)
			}
			if len(rt.emails) != 1 || rt.emails[0] != "shared@x" {
				t.Fatalf("reset calls = %v, want one shared client reset", rt.emails)
			}
			var count int64
			if err := db.Model(&model.NodeClientTraffic{}).Where("node_id = ?", node.Id).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if (count == 1) != fail {
				t.Fatalf("baseline count = %d, failure = %v", count, fail)
			}
		})
	}
}

func TestAuditResetRejectsBrokenReenableAndRollsBackCounters(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	ib := seedLocalDisabledClient(t, svc, 55004, "", "broken@x", 1000, 0, 600, 500)
	if err := database.GetDB().Model(ib).UpdateColumn("settings", "{").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResetTrafficByEmail(&InboundService{}, "broken@x"); err == nil {
		t.Fatal("broken re-enable returned success")
	}
	if recordEnableOf(t, svc, "broken@x") {
		t.Fatal("failed reset re-enabled only the canonical record")
	}
	if tr := trafficOf(t, "broken@x"); tr.Up != 600 || tr.Down != 500 || tr.Enable {
		t.Fatalf("failed reset changed traffic: %+v", tr)
	}
}

func TestAuditScopedResetBatchesSQLAndLeavesOtherInbound(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	ib := mkInbound(t, 55005, model.VLESS, `{"clients":[]}`)
	other := mkInbound(t, 55006, model.VLESS, `{"clients":[]}`)
	var records []model.ClientRecord
	var traffic []xray.ClientTraffic
	for i := range 801 {
		email := fmt.Sprintf("batch-%d@x", i)
		records = append(records, model.ClientRecord{Email: email, Enable: true})
		traffic = append(traffic, xray.ClientTraffic{Email: email, Enable: true, Up: 100})
	}
	if err := db.CreateInBatches(&records, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(&traffic, 100).Error; err != nil {
		t.Fatal(err)
	}
	var links []model.ClientInbound
	for _, rec := range records {
		links = append(links, model.ClientInbound{ClientId: rec.Id, InboundId: ib.Id})
	}
	if err := db.CreateInBatches(&links, 100).Error; err != nil {
		t.Fatal(err)
	}
	mkTraffic(t, other.Id, "untouched@x", 75, 25, 0, 0, true)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Raw(func(driver any) error {
		driver.(*sqlite3.SQLiteConn).SetLimit(sqlite3.SQLITE_LIMIT_VARIABLE_NUMBER, 500)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (&ClientService{}).ResetAllClientTraffics(&InboundService{}, ib.Id); err != nil {
		t.Fatal(err)
	}
	if tr := trafficOf(t, "batch-800@x"); tr.Up != 0 {
		t.Fatalf("last batch not reset: %+v", tr)
	}
	if tr := trafficOf(t, "untouched@x"); tr.Up != 75 || tr.Down != 25 {
		t.Fatalf("other inbound changed: %+v", tr)
	}
}
