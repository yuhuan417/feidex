package feishuapp

import (
	appbackend "feidex/internal/adapter/feishu/backend"
	appautoretry "feidex/internal/application/autoretry"
	"feidex/internal/application/frontend"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"

	"path/filepath"
	"testing"

	"feidex/internal/config"
	"feidex/internal/state"
)

func TestFrontendIdleState(t *testing.T) {
	t.Parallel()

	newTestApp := func(t *testing.T) (*App, *state.Store) {
		t.Helper()
		store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
		if err != nil {
			t.Fatalf("Open(store) error = %v", err)
		}
		return prepareTestApp(&App{
			cfg:        config.Default(),
			store:      store,
			frontendID: "frontend-a",
		}), store
	}

	currentSessionKey := "feishu:frontend:frontend-a:chat:chat-1"
	foreignSessionKey := "feishu:frontend:frontend-b:chat:chat-2"

	tests := []struct {
		name     string
		seed     func(t *testing.T, a *App, store *state.Store)
		wantIdle bool
		want     string
	}{
		{
			name: "idle ignores other frontend state",
			seed: func(t *testing.T, a *App, store *state.Store) {
				t.Helper()
				if err := store.UpsertSession(&conversation.Session{
					Key:    currentSessionKey,
					Status: "idle",
				}); err != nil {
					t.Fatalf("UpsertSession(current) error = %v", err)
				}
				if err := store.UpsertSession(&conversation.Session{
					Key:    foreignSessionKey,
					Status: "idle",
					Queue:  []string{"sub-foreign"},
				}); err != nil {
					t.Fatalf("UpsertSession(foreign) error = %v", err)
				}
				a.runtimeOwner = testOwnerWithAutoRetries(&appautoretry.Tracker{States: map[string]*appautoretry.RetryState{
					foreignSessionKey: {
						SessionKey: foreignSessionKey,
						ThreadID:   "thread-foreign",
						Timer:      &fakeDelayedTask{},
					},
				}})
			},
			wantIdle: true,
			want:     "",
		},
		{
			name: "backend switching blocks idle",
			seed: func(t *testing.T, a *App, _ *state.Store) {
				t.Helper()
				a.runtimeOwner.BackendTransition.BeginBackendSwitchState(domainbackend.BackendCodex)
			},
			want: "当前正在切换到 Codex backend，请稍后再试",
		},
		{
			name: "maintenance blocks idle",
			seed: func(t *testing.T, a *App, _ *state.Store) {
				t.Helper()
				a.bindings.Maintenance.BeginCodexUpgrade(appbackend.BackendUpgradeSnapshot{Running: true})
			},
			want: "当前正在执行 Codex 维护，请稍后再切换 backend",
		},
		{
			name: "in flight message traffic blocks idle",
			seed: func(t *testing.T, a *App, _ *state.Store) {
				t.Helper()
				a.runtimeOwner.BeginMessageTraffic()
				t.Cleanup(func() { a.runtimeOwner.EndMessageTraffic() })
			},
			want: "当前仍有消息处理中",
		},
		{
			name: "claude maintenance blocks idle",
			seed: func(t *testing.T, a *App, _ *state.Store) {
				t.Helper()
				a.bindings.Maintenance.BeginClaudeUpgrade(appbackend.BackendUpgradeSnapshot{Running: true})
			},
			want: "当前正在执行 Claude 维护，请稍后再切换 backend",
		},
		{
			name: "active work blocks idle",
			seed: func(t *testing.T, _ *App, store *state.Store) {
				t.Helper()
				if err := store.UpsertSession(&conversation.Session{
					Key:    currentSessionKey,
					Status: sessionStatusCompacting,
				}); err != nil {
					t.Fatalf("UpsertSession(active) error = %v", err)
				}
			},
			want: "当前仍有运行中的任务",
		},
		{
			name: "queued submissions block idle",
			seed: func(t *testing.T, _ *App, store *state.Store) {
				t.Helper()
				if err := store.UpsertSession(&conversation.Session{
					Key:    currentSessionKey,
					Status: "idle",
					Queue:  []string{"sub-1"},
				}); err != nil {
					t.Fatalf("UpsertSession(queue) error = %v", err)
				}
			},
			want: "当前仍有排队中的消息",
		},
		{
			name: "staged images block idle",
			seed: func(t *testing.T, _ *App, store *state.Store) {
				t.Helper()
				if err := store.UpsertSession(&conversation.Session{
					Key:    currentSessionKey,
					Status: "idle",
					StagedImages: []conversation.SessionStagedImage{{
						SourceMessageID: "img-1",
						Name:            "image.png",
						LocalPath:       "/tmp/image.png",
					}},
				}); err != nil {
					t.Fatalf("UpsertSession(staged) error = %v", err)
				}
			},
			want: "当前仍有暂存图片待提交",
		},
		{
			name: "non idle status blocks idle",
			seed: func(t *testing.T, _ *App, store *state.Store) {
				t.Helper()
				if err := store.UpsertSession(&conversation.Session{
					Key:    currentSessionKey,
					Status: "queued",
				}); err != nil {
					t.Fatalf("UpsertSession(status) error = %v", err)
				}
			},
			want: "当前会话还没有完全回到空闲态",
		},
		{
			name: "pending request blocks idle",
			seed: func(t *testing.T, _ *App, store *state.Store) {
				t.Helper()
				if err := store.UpsertPending(&state.PendingRequest{
					ID:         "req-1",
					FrontendID: "frontend-a",
					Status:     "pending",
				}); err != nil {
					t.Fatalf("UpsertPending() error = %v", err)
				}
			},
			want: "当前仍有待处理审批或表单",
		},
		{
			name: "pending auto retry blocks idle",
			seed: func(t *testing.T, a *App, store *state.Store) {
				t.Helper()
				if err := store.UpsertSession(&conversation.Session{
					Key:    currentSessionKey,
					Status: "idle",
				}); err != nil {
					t.Fatalf("UpsertSession(current) error = %v", err)
				}
				a.runtimeOwner = testOwnerWithAutoRetries(&appautoretry.Tracker{States: map[string]*appautoretry.RetryState{
					currentSessionKey: {
						SessionKey: currentSessionKey,
						ThreadID:   "thread-1",
						Timer:      &fakeDelayedTask{},
					},
				}})
			},
			want: "当前仍有自动重试中的任务",
		},
		{
			name: "running auto retry blocks idle",
			seed: func(t *testing.T, a *App, store *state.Store) {
				t.Helper()
				if err := store.UpsertSession(&conversation.Session{
					Key:    currentSessionKey,
					Status: "idle",
				}); err != nil {
					t.Fatalf("UpsertSession(current) error = %v", err)
				}
				a.runtimeOwner = testOwnerWithAutoRetries(&appautoretry.Tracker{States: map[string]*appautoretry.RetryState{
					currentSessionKey: {
						SessionKey: currentSessionKey,
						ThreadID:   "thread-1",
						RetryCount: 1,
					},
				}})
			},
			want: "当前仍有自动重试中的任务",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, store := newTestApp(t)
			if tt.seed != nil {
				tt.seed(t, a, store)
			}
			recomposeTestApp(a)
			got := frontendIdleBlockedReason(a.bindings.FrontendQuery)
			if got != tt.want {
				t.Fatalf("frontendIdleBlockedReason() = %q, want %q", got, tt.want)
			}
			if got := a.bindings.BackendSelection.BackendSwitchBlockedReason(); got != tt.want {
				t.Fatalf("backendSwitchBlockedReason() = %q, want %q", got, tt.want)
			}
			if got := frontendIsIdle(a.bindings.FrontendQuery); got != tt.wantIdle {
				t.Fatalf("frontendIsIdle() = %v, want %v", got, tt.wantIdle)
			}
		})
	}
}

func TestFrontendIdleStateNilApp(t *testing.T) {
	var query frontend.Query
	if got := frontendIdleBlockedReason(query); got != "app not initialized" {
		t.Fatalf("frontendIdleBlockedReason(nil) = %q", got)
	}
	if frontendIsIdle(query) {
		t.Fatal("frontendIsIdle(nil) = true, want false")
	}
}

func TestFrontendIdleIgnoringCurrentMessage(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	a := prepareTestApp(&App{
		cfg:        config.Default(),
		store:      store,
		frontendID: "frontend-a",
	})
	a.runtimeOwner.
		BeginMessageTraffic()
	if got := frontendIdleBlockedReason(a.bindings.FrontendQuery); got != "当前仍有消息处理中" {
		t.Fatalf("frontendIdleBlockedReason() = %q, want message traffic block", got)
	}
	if got := frontendIdleBlockedReasonIgnoringCurrentMessage(a.bindings.FrontendQuery); got != "" {
		t.Fatalf("frontendIdleBlockedReasonIgnoringCurrentMessage() = %q, want idle", got)
	}
	a.runtimeOwner.
		BeginMessageTraffic()
	if got := frontendIdleBlockedReasonIgnoringCurrentMessage(a.bindings.FrontendQuery); got != "当前仍有消息处理中" {
		t.Fatalf("frontendIdleBlockedReasonIgnoringCurrentMessage() with concurrent traffic = %q, want message traffic block", got)
	}
}
