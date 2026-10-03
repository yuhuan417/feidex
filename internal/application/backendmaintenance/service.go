package backendmaintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"feidex/internal/application/interaction"
	"feidex/internal/domain/backend"
	interactions "feidex/internal/domain/interaction"
	"feidex/internal/textutil"
)

type Installer interface {
	Probe(context.Context) (backend.InstallProbe, error)
	LatestVersion(context.Context) (string, error)
	InstallVersion(context.Context, string) error
}

type State interface {
	Active() bool
	UpgradeState() backend.UpgradeSnapshot
	RestartState() backend.RestartSnapshot
	BeginUpgrade(backend.UpgradeSnapshot) bool
	BeginRestart(backend.RestartSnapshot) bool
	UpdateUpgrade(func(*backend.UpgradeSnapshot)) backend.UpgradeSnapshot
	UpdateRestart(func(*backend.RestartSnapshot)) backend.RestartSnapshot
	FinishUpgrade(string, string) backend.UpgradeSnapshot
	FinishRestart(string, string) backend.RestartSnapshot
}

type Runtime interface {
	Refresh(context.Context) (bool, error)
}
type Progress struct {
	Operation Operation
	Upgrade   *backend.UpgradeSnapshot
	Restart   *backend.RestartSnapshot
}
type Publisher interface {
	Publish(context.Context, Progress)
}
type Operation struct {
	RequestID, MessageID, SessionKey string
	Payload                          backend.UpgradePayload
	Restart                          bool
}
type View struct {
	Probe                                  backend.InstallProbe
	LatestVersion, LatestError, BusyReason string
	Snapshot                               backend.UpgradeSnapshot
	Restart                                backend.RestartSnapshot
}
type Confirmation struct {
	RequestID string
	Payload   backend.UpgradePayload
}
type Service struct {
	Name, Kind string
	Forms      *interaction.FormService
	Installer  func() Installer
	State      State
	BusyReason func() string
	Runtime    Runtime
	Publisher  Publisher
}

func (s Service) View(ctx context.Context, latest bool) (View, error) {
	manager := s.Installer()
	probe, err := manager.Probe(ctx)
	if err != nil {
		return View{}, err
	}
	view := View{Probe: probe, BusyReason: s.BusyReason(), Snapshot: s.State.UpgradeState(), Restart: s.State.RestartState()}
	if latest && probe.Supported && !view.Snapshot.Running && !view.Restart.Running {
		version, err := manager.LatestVersion(ctx)
		if err != nil {
			view.LatestError = err.Error()
		} else {
			view.LatestVersion = strings.TrimSpace(version)
		}
	}
	return view, nil
}

func (s Service) Prepare(sessionKey, userID string, view View) (Confirmation, error) {
	if view.Snapshot.Running || view.Restart.Running || !view.Probe.Supported || view.BusyReason != "" || view.LatestError != "" || view.LatestVersion == "" || (view.LatestVersion != "latest" && view.LatestVersion == view.Probe.CurrentVersion) {
		return Confirmation{}, nil
	}
	payload := backend.UpgradePayload{CurrentVersion: view.Probe.CurrentVersion, TargetVersion: view.LatestVersion, Command: view.Probe.Command, CommandPath: view.Probe.CommandPath, UpdateCommand: view.Probe.UpdateCommand}
	req, err := s.Forms.Open(s.Kind+"-upgrade", interactions.PendingRequest{Kind: s.Kind + "_self_upgrade", SessionKey: sessionKey, OwnerUserID: userID}, payload, 15*time.Minute)
	if err != nil {
		return Confirmation{}, err
	}
	return Confirmation{RequestID: req.ID, Payload: payload}, nil
}

func (s Service) Cancel(id, userID string) (*interactions.PendingRequest, error) {
	return s.Forms.Transition(id, s.Kind+"_self_upgrade", userID, "pending", "resolved", "")
}

func (s Service) Confirm(id, userID, messageID string) (Operation, error) {
	req, err := s.Forms.Transition(id, s.Kind+"_self_upgrade", userID, "pending", "processing", messageID)
	if err != nil {
		return Operation{}, err
	}
	var payload backend.UpgradePayload
	if err := json.Unmarshal([]byte(req.PayloadJSON), &payload); err != nil {
		cause := fmt.Errorf("升级参数损坏: %w", err)
		if err := s.Forms.SaveDraft(id, nil, "pending", 0, ""); err != nil {
			return Operation{}, fmt.Errorf("%w; restore request: %v", cause, err)
		}
		return Operation{}, cause
	}
	snapshot := backend.UpgradeSnapshot{Phase: "preflight", Message: "正在校验升级前置条件", CurrentVersion: payload.CurrentVersion, PreviousVersion: payload.CurrentVersion, TargetVersion: payload.TargetVersion, LatestVersion: payload.TargetVersion}
	if !s.State.BeginUpgrade(snapshot) {
		if err := s.Forms.SaveDraft(id, nil, "pending", 0, ""); err != nil {
			return Operation{}, err
		}
		return Operation{}, fmt.Errorf("%s 正在维护中", s.Name)
	}
	if err := s.Forms.SaveDraft(id, nil, "resolved", 0, ""); err != nil {
		s.State.FinishUpgrade("failed", err.Error())
		return Operation{}, err
	}
	return Operation{RequestID: id, SessionKey: req.SessionKey, MessageID: textutil.FirstNonEmpty(messageID, req.FeishuMsgID), Payload: payload}, nil
}

func (s Service) BeginRestart() (backend.RestartSnapshot, error) {
	if s.State.UpgradeState().Running || s.State.RestartState().Running {
		return backend.RestartSnapshot{}, fmt.Errorf("%s 正在维护中，请稍后再试", s.Name)
	}
	if reason := s.BusyReason(); reason != "" {
		return backend.RestartSnapshot{}, fmt.Errorf("%s", reason)
	}
	snapshot := backend.RestartSnapshot{Phase: "preflight", Message: "正在校验重启前置条件", CurrentVersion: textutil.FirstNonEmpty(s.State.UpgradeState().CurrentVersion, s.State.RestartState().CurrentVersion)}
	if !s.State.BeginRestart(snapshot) {
		return backend.RestartSnapshot{}, fmt.Errorf("%s 正在维护中，请稍后再试", s.Name)
	}
	return s.State.RestartState(), nil
}

func (s Service) Reject(operation Operation, err error) {
	s.finish(context.Background(), operation, "failed", err.Error())
}

func (s Service) update(ctx context.Context, op Operation, phase, message string) {
	progress := Progress{Operation: op}
	if op.Restart {
		snapshot := s.State.UpdateRestart(func(current *backend.RestartSnapshot) { current.Phase, current.Message = phase, message })
		progress.Restart = &snapshot
	} else {
		snapshot := s.State.UpdateUpgrade(func(current *backend.UpgradeSnapshot) { current.Phase, current.Message = phase, message })
		progress.Upgrade = &snapshot
	}
	s.Publisher.Publish(ctx, progress)
}

func (s Service) finish(ctx context.Context, op Operation, result, message string) {
	progress := Progress{Operation: op}
	if op.Restart {
		snapshot := s.State.FinishRestart(result, message)
		progress.Restart = &snapshot
	} else {
		snapshot := s.State.FinishUpgrade(result, message)
		progress.Upgrade = &snapshot
	}
	s.Publisher.Publish(ctx, progress)
}

func (s Service) Run(ctx context.Context, op Operation) {
	manager := s.Installer()
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	probe, err := manager.Probe(probeCtx)
	cancel()
	label := "升级"
	if op.Restart {
		label = "重启"
	}
	fail := func(message string) { s.finish(ctx, op, "failed", message) }
	if err != nil {
		fail(label + "前检查失败: " + err.Error())
		return
	}
	if !op.Restart && !probe.Supported {
		fail("当前环境不支持 " + s.Name + " 自升级: " + textutil.FirstNonEmpty(probe.Reason, "unknown"))
		return
	}
	previous := textutil.FirstNonEmpty(probe.CurrentVersion, op.Payload.CurrentVersion)
	target := textutil.FirstNonEmpty(op.Payload.TargetVersion, "latest")
	if op.Restart {
		s.State.UpdateRestart(func(current *backend.RestartSnapshot) {
			current.CurrentVersion = textutil.FirstNonEmpty(probe.CurrentVersion, current.CurrentVersion)
		})
	} else {
		s.State.UpdateUpgrade(func(current *backend.UpgradeSnapshot) {
			current.CurrentVersion, current.PreviousVersion, current.TargetVersion, current.LatestVersion = previous, previous, target, target
		})
	}
	if reason := s.BusyReason(); reason != "" {
		fail(label + "前检查失败: " + reason)
		return
	}
	installed := previous
	if !op.Restart {
		update := textutil.FirstNonEmpty(probe.UpdateCommand, op.Payload.UpdateCommand, "update")
		s.update(ctx, op, "installing", "正在运行 "+s.Name+" 自升级命令 `"+textutil.FirstNonEmpty(probe.Command, s.Kind)+" "+update+"`")
		installCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		err := manager.InstallVersion(installCtx, "latest")
		cancel()
		if err != nil {
			fail(s.Name + " 自升级失败，未自动回滚: " + err.Error())
			return
		}
		probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		after, err := manager.Probe(probeCtx)
		cancel()
		if err != nil {
			fail(s.Name + " 自升级后版本检查失败，未自动回滚: " + err.Error())
			return
		}
		installed = textutil.FirstNonEmpty(after.CurrentVersion, previous)
		s.State.UpdateUpgrade(func(current *backend.UpgradeSnapshot) {
			current.TargetVersion, current.LatestVersion = installed, installed
		})
	} else {
		s.update(ctx, op, "restarting", "正在准备新的 "+s.Name+" runtime")
	}
	s.update(ctx, op, "smoke_testing", "正在验证 "+s.Name+" runtime")
	refreshCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	switched, err := s.Runtime.Refresh(refreshCtx)
	cancel()
	if err != nil {
		if op.Restart {
			fail(s.Name + " runtime 重启失败: " + err.Error())
		} else {
			fail(s.Name + " 自升级后 runtime 验证失败，未自动回滚: " + err.Error())
		}
		return
	}
	var message string
	switch {
	case op.Restart && switched:
		message = s.Name + " runtime 已原地重启，后续任务会使用新进程"
	case op.Restart:
		message = s.Name + " CLI 校验通过；当前 frontend 未启用 " + s.Name + " backend"
	case installed != "" && installed == previous && switched:
		message = s.Name + " 已是最新版本 `" + installed + "`，runtime 已重新验证"
	case installed != "" && installed == previous:
		message = s.Name + " 已是最新版本 `" + installed + "`；当前 frontend 未启用 " + s.Name + " backend"
	case switched:
		message = s.Name + " 自升级成功，已切换到 `" + textutil.FirstNonEmpty(installed, target) + "`"
	default:
		message = s.Name + " 自升级成功，已验证 `" + textutil.FirstNonEmpty(installed, target) + "` 可用；当前 frontend 未启用 " + s.Name + " backend"
	}
	s.finish(ctx, op, "success", message)
}
