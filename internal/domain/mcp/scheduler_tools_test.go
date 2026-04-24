package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSchedulerQuery_NoArgs(t *testing.T) {
	resetSchedulerStoreForTest()

	got, err := handleSchedulerQueryTool(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(got), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp["total"] != float64(0) {
		t.Fatalf("expected total=0, got=%v", resp["total"])
	}
}

func TestSchedulerMutation_CreateThenQuery(t *testing.T) {
	resetSchedulerStoreForTest()

	createArgs := `{"op":"create","payload":{"name":"daily","schedule_type":"cron","cron_expr":"0 0 8 * * *"}}`
	if _, err := handleSchedulerMutationTool(context.Background(), createArgs); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := handleSchedulerQueryTool(context.Background(), "")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(got), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp["total"] != float64(1) {
		t.Fatalf("expected total=1, got=%v", resp["total"])
	}
}

func resetSchedulerStoreForTest() {
	defaultSchedulerStore.mu.Lock()
	defer defaultSchedulerStore.mu.Unlock()
	defaultSchedulerStore.jobs = make(map[string]schedulerJob)
}
