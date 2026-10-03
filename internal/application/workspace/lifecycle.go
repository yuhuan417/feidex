package workspace

import (
	"fmt"
	"strings"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/routing"
	domain "feidex/internal/domain/workspace"
)

// LifecycleState is a detached read of workspace references. Sessions includes
// other frontends because workspace definitions are machine-wide resources.
type LifecycleState struct {
	Sessions []*conversation.Session
	Bindings []*routing.AgentBinding
	Profiles []*routing.BotProfile
}

// LifecycleChange is committed atomically in the state repository. Expected
// prevents stale lifecycle decisions from overwriting concurrent input/turns.
// DeleteWorkspace couples reference cleanup to the configuration-file commit.
type LifecycleChange struct {
	Expected          LifecycleState
	Sessions          []*conversation.Session
	Binding           *routing.AgentBinding
	Profiles          []*routing.BotProfile
	DeleteWorkspace   string
	FallbackWorkspace string
}

type LifecycleRepository interface {
	State() LifecycleState
	Commit(LifecycleChange) error
}

type Lifecycle struct {
	Frontend      identity.FrontendID
	Selection     SelectionService
	Configuration ConfigurationService
	Repository    LifecycleRepository
}

// LifecycleEffects contains only committed work. Adapters execute cleanup and
// binding after success; card callbacks can schedule binding asynchronously.
type LifecycleEffects struct {
	ClearLiveThreads []string
	Session          *conversation.Session
	Binding          *routing.AgentBinding
	Workspace        *domain.Workspace
}

type SwitchRequest struct {
	Session     *conversation.Session
	WorkspaceID string
	// Binding changes the current Bot's group association in the same commit.
	Binding *routing.AgentBinding
	Unbind  bool
}

func SwitchBlockedReason(sess *conversation.Session, hasInFlight bool) string {
	if sess == nil {
		return ""
	}
	if hasInFlight || conversation.HasActiveWork(conversation.CloneSession(sess)) {
		return "当前任务仍在运行，请先等待结束或中断后再切换工作区"
	}
	if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return "当前还有待处理消息，请先处理完成后再切换工作区"
	}
	return ""
}

func (l Lifecycle) Switch(req SwitchRequest) (LifecycleEffects, error) {
	if l.Repository == nil {
		return LifecycleEffects{}, fmt.Errorf("workspace lifecycle repository is nil")
	}
	if l.Configuration.Repository == nil {
		return LifecycleEffects{}, fmt.Errorf("workspace configuration repository is nil")
	}
	state := l.Repository.State()
	sess := req.Session
	// Always decide from the latest persisted snapshot, including the window
	// between callback parsing and this use case.
	if sess != nil {
		frontend, _, _, _, _ := identity.ParseSessionKey(sess.Key)
		if frontend != "" && frontend != string(l.Frontend) {
			return LifecycleEffects{}, fmt.Errorf("session frontend does not match scope")
		}
		for _, current := range state.Sessions {
			if current != nil && current.Key == sess.Key {
				sess = current
				break
			}
		}
	}
	// A group binding stores the workspace for subsequent inputs. Existing
	// queued submissions already carry workspace snapshots, and active work
	// keeps its original conversation. Only an actual session switch/unbind
	// requires idle admission and invalidates runtime lineage.
	bindingSelection := req.Binding != nil && !req.Unbind
	if !bindingSelection {
		if reason := SwitchBlockedReason(sess, false); reason != "" {
			return LifecycleEffects{}, fmt.Errorf("%s", reason)
		}
	}
	id := strings.TrimSpace(req.WorkspaceID)
	var ws *domain.Workspace
	var err error
	if req.Unbind {
		if req.Binding == nil || strings.TrimSpace(req.Binding.WorkspaceID) == "" {
			return LifecycleEffects{}, fmt.Errorf("当前群没有已绑定的 workspace")
		}
		id = ""
	} else {
		if id == "" {
			return LifecycleEffects{}, fmt.Errorf("请指定 workspace_id")
		}
		ws, err = l.Configuration.Repository.Get(id)
		if err != nil {
			return LifecycleEffects{}, err
		}
		if ws == nil {
			return LifecycleEffects{}, fmt.Errorf("workspace %q not found", id)
		}
	}
	change := LifecycleChange{Expected: state}
	effects := LifecycleEffects{Workspace: ws}
	if sess != nil && !bindingSelection {
		candidate := conversation.CloneSession(sess)
		conversation.SwitchSessionWorkspace(candidate, id)
		change.Sessions = append(change.Sessions, candidate)
		effects.Session = candidate
		effects.ClearLiveThreads = append(effects.ClearLiveThreads, candidate.Key)
		if req.Binding == nil {
			// The selection record and private profile are committed together with
			// conversation lineage, never as preceding independent writes.
			selection, profile := l.Selection.Transition(candidate.ChatType, candidate.ChatID, candidate.OwnerUserID, id)
			if selection != nil && selection.Key != candidate.Key {
				change.Sessions = append(change.Sessions, selection)
			}
			if profile != nil {
				change.Profiles = append(change.Profiles, profile)
			}
		}
	}
	if req.Binding != nil {
		binding := *req.Binding
		if binding.FrontendID != "" && binding.FrontendID != string(l.Frontend) {
			return LifecycleEffects{}, fmt.Errorf("binding frontend does not match scope")
		}
		// Pick up pending replay/model changes made since the caller read binding.
		for _, current := range state.Bindings {
			if current != nil && current.ID == binding.ID {
				binding = *current
				break
			}
		}
		binding.WorkspaceID = id
		binding.Status = "active"
		if req.Unbind {
			binding.Status = "pending"
		}
		change.Binding = &binding
		effects.Binding = &binding
	}
	if err := l.Repository.Commit(change); err != nil {
		return LifecycleEffects{}, err
	}
	return effects, nil
}

func (l Lifecycle) CreateAndSwitch(req SwitchRequest, input domain.Workspace) (LifecycleEffects, error) {
	if reason := SwitchBlockedReason(req.Session, false); reason != "" {
		return LifecycleEffects{}, fmt.Errorf("%s", reason)
	}
	ws, err := l.Configuration.Create(input)
	if err != nil {
		return LifecycleEffects{}, err
	}
	// A created definition/directory is deliberately retained on switch failure.
	// Clone/worktree callers can retry binding without re-running git.
	req.WorkspaceID = ws.ID
	return l.Switch(req)
}

func (l Lifecycle) ValidateDeletion(sessionKey, id string) error {
	_, _, err := l.deletion(sessionKey, id)
	return err
}

func (l Lifecycle) deletion(sessionKey, id string) (LifecycleState, string, error) {
	if l.Repository == nil || l.Configuration.Repository == nil {
		return LifecycleState{}, "", fmt.Errorf("workspace lifecycle repository is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return LifecycleState{}, "", fmt.Errorf("请指定 workspace_id")
	}
	ws, err := l.Configuration.Repository.Get(id)
	if err != nil {
		return LifecycleState{}, "", err
	}
	if ws == nil {
		return LifecycleState{}, "", fmt.Errorf("workspace %q 不存在", id)
	}
	workspaces := l.Configuration.Repository.List()
	if len(workspaces) <= 1 {
		return LifecycleState{}, "", fmt.Errorf("至少保留一个 workspace")
	}
	state := l.Repository.State()
	var current *conversation.Session
	for _, sess := range state.Sessions {
		if sess != nil && sess.Key == sessionKey {
			current = sess
			break
		}
	}
	if id == l.Selection.ResolveSession(current) {
		return LifecycleState{}, "", fmt.Errorf("不能删除当前 workspace，请先切换到其他 workspace")
	}
	for _, sess := range state.Sessions {
		if sess != nil && ReferencesWorkspace(sess, id) && SwitchBlockedReason(sess, false) != "" {
			return LifecycleState{}, "", fmt.Errorf("workspace %q 仍有运行中或待处理的任务，无法删除", id)
		}
	}
	for _, binding := range state.Bindings {
		if binding != nil && strings.TrimSpace(binding.WorkspaceID) == id {
			return LifecycleState{}, "", fmt.Errorf("workspace %q 仍被某个群里的当前 Bot 工作区配置使用，请先在对应群聊中用 /workspace use 切换", id)
		}
	}
	for _, ws := range workspaces {
		if ws.ID != id {
			return state, ws.ID, nil
		}
	}
	return LifecycleState{}, "", fmt.Errorf("至少保留一个 workspace")
}

func (l Lifecycle) Delete(sessionKey, id string) (LifecycleEffects, error) {
	state, fallback, err := l.deletion(sessionKey, id)
	if err != nil {
		return LifecycleEffects{}, err
	}
	id = strings.TrimSpace(id)
	change := LifecycleChange{Expected: state, DeleteWorkspace: id, FallbackWorkspace: fallback}
	effects := LifecycleEffects{}
	for _, sess := range state.Sessions {
		if sess == nil || !ReferencesWorkspace(sess, id) {
			continue
		}
		candidate := conversation.CloneSession(sess)
		if strings.TrimSpace(candidate.WorkspaceID) == id {
			conversation.SwitchSessionWorkspace(candidate, fallback)
		} else if strings.TrimSpace(candidate.ActiveThreadWorkspaceID) == id {
			conversation.ClearThreadContext(candidate)
		}
		for backend, thread := range candidate.BackendThreads {
			if strings.TrimSpace(thread.WorkspaceID) == id {
				conversation.ClearBackendThread(candidate, backend)
			}
		}
		change.Sessions = append(change.Sessions, candidate)
		effects.ClearLiveThreads = append(effects.ClearLiveThreads, candidate.Key)
	}
	for _, profile := range state.Profiles {
		if profile != nil && strings.TrimSpace(profile.WorkspaceID) == id {
			candidate := *profile
			candidate.WorkspaceID = fallback
			change.Profiles = append(change.Profiles, &candidate)
		}
	}
	if err := l.Repository.Commit(change); err != nil {
		return LifecycleEffects{}, err
	}
	return effects, nil
}

func ReferencesWorkspace(sess *conversation.Session, id string) bool {
	if SessionReferencesWorkspace(sess, id) {
		return true
	}
	if sess != nil {
		for _, thread := range sess.BackendThreads {
			if strings.TrimSpace(thread.WorkspaceID) == strings.TrimSpace(id) {
				return true
			}
		}
	}
	return false
}

// BindingCandidate validates a delayed binding effect against current state.
// A callback may already have been followed by another switch or new input.
func (l Lifecycle) BindingCandidate(sessionKey, workspaceID string) (*conversation.Session, *domain.Workspace, error) {
	if l.Repository == nil || l.Configuration.Repository == nil {
		return nil, nil, fmt.Errorf("workspace lifecycle repository is nil")
	}
	for _, sess := range l.Repository.State().Sessions {
		if sess == nil || sess.Key != sessionKey {
			continue
		}
		if SwitchBlockedReason(sess, false) != "" || strings.TrimSpace(sess.WorkspaceID) != strings.TrimSpace(workspaceID) {
			return nil, nil, nil
		}
		if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == strings.TrimSpace(workspaceID) {
			return nil, nil, nil
		}
		ws, err := l.Configuration.Repository.Get(workspaceID)
		if err != nil || ws == nil {
			return nil, nil, err
		}
		return conversation.CloneSession(sess), ws, nil
	}
	return nil, nil, nil
}
