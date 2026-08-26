package extension

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func TestHostTaskHandlersAndCommitBeforeNotify(t *testing.T) {
	h := NewHost(nil)
	defer h.Close(context.Background())
	if got := h.routeHostCall(nil, nil, nil, sdk.HostCallRequest{Method: sdk.MethodTasksGet, Params: []byte(`{}`)}); got.Error == "" {
		t.Fatal("expected missing ledger error")
	}
	var delivered sdk.Message
	h.SetAgentBridge(&testAgentBridge{onDeliver: func(_ string, msg sdk.Message, wake bool) error {
		delivered = msg
		if !wake {
			t.Fatal("expected wake")
		}
		return errors.New("offline")
	}})
	if err := h.SetTaskLedgerDirectory(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	call := func(method string, v any) sdk.HostCallResponse {
		b, _ := json.Marshal(v)
		return h.routeHostCall(nil, nil, nil, sdk.HostCallRequest{Method: method, Params: b})
	}
	lr := call(sdk.MethodTasklistCreate, sdk.TasklistCreateRequest{Name: "x", OwnerAgentID: "owner"})
	if lr.Error != "" {
		t.Fatal(lr.Error)
	}
	var l sdk.TaskListResponse
	json.Unmarshal(lr.Result, &l)
	tr := call(sdk.MethodTasksCreate, sdk.TasksCreateRequest{ListID: l.List.ListID, Title: "x", OwnerAgentID: "owner"})
	if tr.Error != "" {
		t.Fatal(tr.Error)
	}
	var task sdk.TaskRecordResponse
	json.Unmarshal(tr.Result, &task)
	cr := call(sdk.MethodTasksClaim, sdk.TasksClaimRequest{ListID: l.List.ListID, TaskID: task.Task.TaskID, AgentID: "worker"})
	if cr.Error != "" {
		t.Fatal(cr.Error)
	}
	var claimed sdk.TaskRecordResponse
	json.Unmarshal(cr.Result, &claimed)
	if delivered.Content == "" {
		t.Fatal("notification was not attempted")
	}
	got := call(sdk.MethodTasksGet, sdk.TasksGetRequest{ListID: l.List.ListID, TaskID: task.Task.TaskID})
	if got.Error != "" {
		t.Fatalf("committed task unavailable after notify failure: %s", got.Error)
	}
}

func TestHostTaskHandlersRejectTrailingJSON(t *testing.T) {
	h := NewHost(nil)
	defer h.Close(context.Background())
	h.SetTaskLedgerDirectory(t.TempDir())
	r := h.routeHostCall(nil, nil, nil, sdk.HostCallRequest{Method: sdk.MethodTasklistCreate, Params: []byte(`{"name":"x"}{}`)})
	if r.Error == "" {
		t.Fatal("expected malformed parameter error")
	}
}
