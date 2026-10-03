package codex

import (
	"context"
	"reflect"
	"testing"

	"feidex/internal/application/backendops"
	"feidex/internal/codexrpc"
)

func TestThreadStartParamsEncodeSemanticConfiguration(t *testing.T) {
	params := ThreadStartParams(backendops.ThreadStartConfig{
		Cwd: "/repo", ApprovalPolicy: "never", SandboxMode: "workspace-write",
		ServiceName: "feidex", ExperimentalRawEvents: true, PersistExtendedHistory: true,
		ServiceTier: "fast", Model: "gpt", AuxiliaryConfig: map[string]any{"x": true},
	})
	want := map[string]any{
		"cwd": "/repo", "approvalPolicy": "never", "sandbox": "workspace-write",
		"serviceName": "feidex", "experimentalRawEvents": true,
		"persistExtendedHistory": true, "serviceTier": "fast", "model": "gpt",
		"config": map[string]any{"x": true},
	}
	if got := params.Map(); !reflect.DeepEqual(got, want) {
		t.Fatalf("thread/start params = %#v, want %#v", got, want)
	}
}

func TestThreadStartParamsOmitOptionalFields(t *testing.T) {
	got := ThreadStartParams(backendops.ThreadStartConfig{Cwd: " /repo "}).Map()
	want := map[string]any{
		"cwd": "/repo", "approvalPolicy": "", "sandbox": "", "serviceName": "",
		"experimentalRawEvents": false, "persistExtendedHistory": false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("thread/start params = %#v, want %#v", got, want)
	}
}

func TestThreadForkParamsEncodeOptionalFields(t *testing.T) {
	got := ThreadForkParams(backendops.ThreadForkRequest{
		ThreadID: " thread ", Cwd: " /repo ", ApprovalPolicy: " never ", SandboxMode: " workspace-write ",
		ServiceTier: "fast", Model: "gpt", MultiAgentMode: "plan",
	})
	want := map[string]any{"threadId": "thread", "cwd": "/repo", "approvalPolicy": "never", "sandbox": "workspace-write", "serviceTier": "fast", "model": "gpt", "multiAgentMode": "plan"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("thread/fork params = %#v, want %#v", got, want)
	}
	got = ThreadForkParams(backendops.ThreadForkRequest{ThreadID: "thread"})
	if _, ok := got["model"]; ok {
		t.Fatal("empty model must be omitted from thread/fork")
	}
}

func TestResumeThreadMapsAppliedModelConfiguration(t *testing.T) {
	client := gatewayRPCFunc(func(_ context.Context, method string, in, out any) error {
		if method != "thread/resume" {
			t.Fatalf("method = %q", method)
		}
		params := in.(map[string]any)
		if params["threadId"] != "thread" || params["model"] != "gpt" {
			t.Fatalf("params = %#v", params)
		}
		out.(*codexrpc.ThreadStartResult).Thread.ID = "thread"
		return nil
	})
	result, err := ResumeThread(context.Background(), client, " thread ", " gpt ", map[string]any{"x": true})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "thread" || !result.Applied.Valid || result.Applied.Model != "gpt" {
		t.Fatalf("result = %+v", result)
	}
}
