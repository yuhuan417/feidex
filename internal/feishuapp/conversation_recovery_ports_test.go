package feishuapp

import (
	"context"
	"sync"
	"testing"

	appconversation "feidex/internal/application/conversation"
	"feidex/internal/config"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	frontendruntime "feidex/internal/runtime"
	codexruntime "feidex/internal/runtime/codex"
)

func TestConversationRecoveryPortsCaptureFrontendRuntimeEndpoint(t *testing.T) {
	cfg := config.Default()
	cfg.Feishu.Backend = domainbackend.BackendClaude
	mu := &sync.RWMutex{}
	owner := frontendruntime.NewFrontendOwner()
	owner.SetBackend(domainbackend.BackendCodex)
	firstClient := &fakeCodexClient{}
	owner.SetCodexClient(firstClient)
	recovery := codexruntime.NewRecoveryService(codexruntime.RecoveryDependencies{State: owner.CodexRecovery, IsBackendActive: func() bool { return true }})
	deps := ConversationRecoveryPorts(cfg, mu, -1, nil, nil, owner, recovery, nil)
	if deps.Repository != nil || deps.Conversations != nil {
		t.Fatal("recovery retained values other than the supplied dependencies")
	}
	if got := deps.Workspaces.DefaultID(); got != cfg.Workspaces[0].ID {
		t.Fatalf("default workspace = %q", got)
	}
	endpoint, err := deps.Capture()
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.LazyResume || endpoint.Gateway == nil || endpoint.Current == nil || !endpoint.Current() {
		t.Fatalf("Codex recovery endpoint = %+v", endpoint)
	}
	if !recovery.BeginRecovery(firstClient) {
		t.Fatal("expected to begin recovery for captured client")
	}
	if endpoint.Current() {
		t.Fatal("endpoint remained current while its Codex client was recovering")
	}
	owner.SetCodexClient(&fakeCodexClient{})
	if endpoint.Current() {
		t.Fatal("endpoint remained current after client replacement")
	}
	owner.SetBackend(domainbackend.BackendClaude)
	claudeEndpoint, err := deps.Capture()
	if err != nil {
		t.Fatal(err)
	}
	if !claudeEndpoint.LazyResume || claudeEndpoint.Gateway != nil {
		t.Fatalf("Claude recovery endpoint = %+v", claudeEndpoint)
	}
	owner.SetBackend(domainbackend.BackendCodex)
	owner.SetCodexClient(nil)
	if _, err := deps.Capture(); err == nil {
		t.Fatal("Codex recovery endpoint captured without a client")
	}
}

func TestConversationRecoveryPortsGatewayUsesCapturedClient(t *testing.T) {
	cfg := config.Default()
	owner := frontendruntime.NewFrontendOwner()
	owner.SetBackend(domainbackend.BackendCodex)
	firstClient := &fakeCodexClient{}
	owner.SetCodexClient(firstClient)
	var method string
	firstClient.callHook = func(_ context.Context, gotMethod string, _ any, _ any) error {
		method = gotMethod
		return nil
	}
	recovery := codexruntime.NewRecoveryService(codexruntime.RecoveryDependencies{State: owner.CodexRecovery, IsBackendActive: func() bool { return true }})
	deps := ConversationRecoveryPorts(cfg, &sync.RWMutex{}, -1, nil, nil, owner, recovery, nil)
	endpoint, err := deps.Capture()
	if err != nil {
		t.Fatal(err)
	}
	secondClient := &fakeCodexClient{}
	owner.SetCodexClient(secondClient)
	_, err = endpoint.Gateway.Resume(context.Background(), appconversation.Request{
		Selection: conversation.ThreadSelection{ThreadID: "captured-thread"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if method != "thread/resume" {
		t.Fatalf("resume used method %q", method)
	}
	if endpoint.Current() {
		t.Fatal("captured endpoint accepted a client that was already replaced")
	}
}
