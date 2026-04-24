package app

import (
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
		return &App{
			cfg:        config.Default(),
			store:      store,
			frontendID: "frontend-a",
		}, store
	}

	currentSessionKey := "feishu:frontend:frontend-a:p2p:chat-1:user-1"
	foreignSessionKey := "feishu:frontend:frontend-b:p2p:chat-2:user-2"

	tests := []struct {
		name     string
		seed     func(t *testing.T, a *App, store *state.Store)
		wantIdle bool
		want     string
	}{
		{
			name: "idle ignores other frontend state",
			seed: func(t *testing.T, _ *App, store *state.Store) {
				t.Helper()
				if err := store.UpsertSession(&state.Session{
					Key:    currentSessionKey,
					Status: "idle",
				}); err != nil {
					t.Fatalf("UpsertSession(current) error = %v", err)
				}
				if err := store.UpsertSession(&state.Session{
					Key:    foreignSessionKey,
					Status: "idle",
					Queue:  []string{"sub-foreign"},
				}); err != nil {
					t.Fatalf("UpsertSession(foreign) error = %v", err)
				}
			},
			wantIdle: true,
			want:     "",
		},
		{
			name: "backend switching blocks idle",
			seed: func(t *testing.T, a *App, _ *state.Store) {
				t.Helper()
				a.beginBackendSwitchState(backendCodex)
			},
			want: "当前正在切换到 Codex backend，请稍后再试",
		},
		{
			name: "maintenance blocks idle",
			seed: func(t *testing.T, a *App, _ *state.Store) {
				t.Helper()
				a.codexMaintenanceTracker().upgrade = codexUpgradeSnapshot{Running: true}
			},
			want: "当前正在执行 Codex 维护，请稍后再切换 backend",
		},
		{
			name: "in flight message traffic blocks idle",
			seed: func(t *testing.T, a *App, _ *state.Store) {
				t.Helper()
				a.beginFrontendMessageTraffic()
				t.Cleanup(a.finishFrontendMessageTraffic)
			},
			want: "当前仍有消息处理中",
		},
		{
			name: "claude maintenance blocks idle",
			seed: func(t *testing.T, a *App, _ *state.Store) {
				t.Helper()
				a.claudeMaintenanceTracker().upgrade = claudeUpgradeSnapshot{Running: true}
			},
			want: "当前正在执行 Claude 维护，请稍后再切换 backend",
		},
		{
			name: "active work blocks idle",
			seed: func(t *testing.T, _ *App, store *state.Store) {
				t.Helper()
				if err := store.UpsertSession(&state.Session{
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
				if err := store.UpsertSession(&state.Session{
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
				if err := store.UpsertSession(&state.Session{
					Key:    currentSessionKey,
					Status: "idle",
					StagedImages: []state.SessionStagedImage{{
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
				if err := store.UpsertSession(&state.Session{
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
			seed: func(t *testing.T, a *App, _ *state.Store) {
				t.Helper()
				a.autoRetries = &autoRetryTracker{states: map[string]*autoRetryState{
					currentSessionKey: {
						SessionKey: currentSessionKey,
						ThreadID:   "thread-1",
						Timer:      &fakeDelayedTask{},
					},
				}}
			},
			want: "当前仍有等待自动重试的任务",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, store := newTestApp(t)
			if tt.seed != nil {
				tt.seed(t, a, store)
			}
			got := a.frontendIdleBlockedReason()
			if got != tt.want {
				t.Fatalf("frontendIdleBlockedReason() = %q, want %q", got, tt.want)
			}
			if got := a.backendSwitchBlockedReason(); got != tt.want {
				t.Fatalf("backendSwitchBlockedReason() = %q, want %q", got, tt.want)
			}
			if got := a.frontendIsIdle(); got != tt.wantIdle {
				t.Fatalf("frontendIsIdle() = %v, want %v", got, tt.wantIdle)
			}
		})
	}
}

func TestFrontendIdleStateNilApp(t *testing.T) {
	var a *App
	if got := a.frontendIdleBlockedReason(); got != "app not initialized" {
		t.Fatalf("frontendIdleBlockedReason(nil) = %q", got)
	}
	if a.frontendIsIdle() {
		t.Fatal("frontendIsIdle(nil) = true, want false")
	}
}
