package store

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"all_notify/internal/model"
)

func TestPruneSendLogsByAgeAndMaxRows(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	entries := []model.SendLog{
		logEntry("old", now.AddDate(0, 0, -3)),
		logEntry("recent-1", now.Add(-time.Hour)),
		logEntry("recent-2", now),
	}
	for _, entry := range entries {
		if err := st.InsertSendLog(context.Background(), entry); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.PruneSendLogs(context.Background(), model.Settings{LogRetentionDays: 1, LogMaxRows: 1}); err != nil {
		t.Fatal(err)
	}
	logs, err := st.ListSendLogs(context.Background(), LogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].ID != "recent-2" {
		t.Fatalf("unexpected logs after prune: %+v", logs)
	}
}

func TestListRoutesReturnsTargetIDsWithoutNestedQueryDeadlock(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	target, err := st.CreateTarget(context.Background(), model.Target{
		Name:    "bark",
		Type:    model.TargetTypeBark,
		Config:  `{"device_key":"x"}`,
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_, err := st.CreateRoute(context.Background(), model.Route{
			Key:       "route-" + strconv.Itoa(i),
			Name:      "Route",
			Enabled:   true,
			TargetIDs: []int64{target.ID},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	routes, err := st.ListRoutes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 3 {
		t.Fatalf("got %d routes", len(routes))
	}
	for _, route := range routes {
		if len(route.TargetIDs) != 1 || route.TargetIDs[0] != target.ID {
			t.Fatalf("route target ids not loaded: %+v", routes)
		}
	}
}

func logEntry(id string, createdAt time.Time) model.SendLog {
	return model.SendLog{
		ID:             id,
		RouteID:        1,
		RouteKey:       "test",
		Title:          "title",
		Message:        "message",
		RequestMethod:  "POST",
		RequestBody:    "{}",
		Status:         model.StatusSuccess,
		TotalTargets:   1,
		SuccessTargets: 1,
		FailedTargets:  0,
		DurationMS:     1,
		RemoteAddr:     "127.0.0.1",
		CreatedAt:      createdAt,
	}
}
