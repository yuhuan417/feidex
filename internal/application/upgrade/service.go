package upgrade

import (
	"context"
	"encoding/json"
	interactionapp "feidex/internal/application/interaction"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/workspace"
	"feidex/internal/textutil"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const PendingKind = "upgrade_release"
const LocalPickerKind = "upgrade_local_binary"

type Environment struct {
	Version, GOOS, GOARCH, Executable, AssetName, ServiceName string
	Installed, Running                                        bool
	PID, ProcessPID                                           int
}
type Release struct {
	Version, ReleaseTag, SourceCommit, BinaryURL, ExpectedSHA256, HTMLURL string
	PublishedAt                                                           time.Time
}
type Platform interface {
	Inspect(context.Context, bool) (Environment, error)
}
type Releases interface {
	Query(context.Context, string, string, bool) (Release, error)
	Compare(string, string) (int, error)
}
type Artifact struct {
	Path, Name, SHA256 string
	Size               int64
}
type Artifacts interface {
	Stage(context.Context, string, string) (Artifact, error)
	Picker(*workspace.Workspace) (workspace.PathPickerPayload, error)
	Resolve(*workspace.Workspace, string) (string, error)
}
type LaunchSpec struct{ OperationID, UnitName, ServiceName, Version, BinaryPath, DownloadURL, SourcePath, ExpectedSHA256 string }
type Launcher interface {
	Launch(context.Context, LaunchSpec) (string, error)
}
type Selection struct {
	Environment                  Environment
	Target                       Release
	Request                      *interaction.PendingRequest
	Payload                      Payload
	Forced, Development, Current bool
	QueryError                   error
}
type Service struct {
	Forms     *interactionapp.FormService
	Platform  Platform
	Releases  Releases
	Artifacts Artifacts
	Launcher  Launcher
}

func (s Service) environment(ctx context.Context, required bool) (Environment, error) {
	env, err := s.Platform.Inspect(ctx, required)
	if err != nil {
		return env, err
	}
	if required {
		if env.GOOS != "linux" {
			return env, fmt.Errorf("当前平台不支持 daemon 自动升级")
		}
		if !env.Installed || !env.Running {
			return env, fmt.Errorf("当前 daemon 未安装或未运行")
		}
		if env.PID > 0 && env.PID != env.ProcessPID {
			return env, fmt.Errorf("当前进程不是 daemon 服务进程，无法执行远程升级")
		}
	}
	return env, nil
}

func (s Service) Prepare(ctx context.Context, sessionKey, userID, version string, dev bool) (Selection, error) {
	env, err := s.environment(ctx, false)
	if err != nil {
		return Selection{}, err
	}
	if env.GOOS == "linux" {
		env, err = s.environment(ctx, true)
		if err != nil {
			return Selection{}, err
		}
	}
	result := Selection{Environment: env, Forced: strings.TrimSpace(version) != "", Development: dev}
	queryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result.Target, err = s.Releases.Query(queryCtx, version, env.GOARCH, dev)
	if err != nil {
		if env.GOOS == "linux" && (dev || result.Forced) {
			return Selection{}, err
		}
		result.QueryError = err
		return result, nil
	}
	if !result.Forced && !dev {
		cmp, err := s.Releases.Compare(env.Version, result.Target.Version)
		result.Current = err == nil && cmp >= 0
	}
	if env.GOOS != "linux" || result.Current {
		return result, nil
	}
	target := result.Target
	result.Payload = Payload{CurrentVersion: env.Version, TargetVersion: target.Version, ReleaseTag: target.ReleaseTag, BinaryPath: env.Executable, DownloadURL: target.BinaryURL, SourceCommit: target.SourceCommit, ExpectedSHA256: target.ExpectedSHA256, ReleaseURL: target.HTMLURL}
	result.Request, err = s.Forms.Open("upgrade", interaction.PendingRequest{Kind: PendingKind, SessionKey: sessionKey, OwnerUserID: userID}, result.Payload, 30*time.Minute)
	return result, err
}

func (s Service) OpenPicker(ctx context.Context, key string, ws *workspace.Workspace, userID, messageID string) (*interaction.PendingRequest, workspace.PathPickerPayload, error) {
	if _, err := s.environment(ctx, true); err != nil {
		return nil, workspace.PathPickerPayload{}, err
	}
	payload, err := s.Artifacts.Picker(ws)
	if err != nil {
		return nil, payload, err
	}
	req, err := s.Forms.Open("upgrade-local", interaction.PendingRequest{Kind: LocalPickerKind, SessionKey: key, OwnerUserID: userID, FeishuMsgID: messageID}, payload, 10*time.Minute)
	return req, payload, err
}

func (s Service) PrepareLocal(ctx context.Context, key, userID, messageID, sourcePath string) (*interaction.PendingRequest, Payload, error) {
	env, err := s.environment(ctx, true)
	if err != nil {
		return nil, Payload{}, err
	}
	id, err := s.Forms.Repository.NextLocalID("upgrade")
	if err != nil {
		return nil, Payload{}, err
	}
	artifact, err := s.Artifacts.Stage(ctx, id, sourcePath)
	if err != nil {
		return nil, Payload{}, err
	}
	payload := Payload{CurrentVersion: env.Version, TargetVersion: filepath.Base(sourcePath), BinaryPath: env.Executable, SourcePath: artifact.Path, SourceKind: "local_file", SourceName: artifact.Name, SourceSize: artifact.Size, ExpectedSHA256: artifact.SHA256}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, Payload{}, err
	}
	req := &interaction.PendingRequest{ID: id, Kind: PendingKind, SessionKey: key, OwnerUserID: userID, FeishuMsgID: messageID, PayloadJSON: string(encoded), Status: "pending", CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(30 * time.Minute).Unix()}
	if err := s.Forms.Repository.SavePending(req); err != nil {
		return nil, Payload{}, err
	}
	return s.Forms.Repository.Pending(id), payload, nil
}

func (s Service) Cancel(id, userID string) (*interaction.PendingRequest, error) {
	return s.Forms.Transition(id, PendingKind, userID, "pending", "resolved", "")
}

func (s Service) AttachDelivery(id, messageID string) error {
	return s.Forms.SaveDraft(id, nil, "", 0, messageID)
}

func (s Service) SelectLocal(ctx context.Context, id, userID, messageID, sourcePath string) (*interaction.PendingRequest, Payload, error) {
	picker, err := s.Forms.Transition(id, LocalPickerKind, userID, "pending", "processing", messageID)
	if err != nil {
		return nil, Payload{}, err
	}
	req, payload, err := s.PrepareLocal(ctx, picker.SessionKey, picker.OwnerUserID, textutil.FirstNonEmpty(picker.FeishuMsgID, messageID), sourcePath)
	if err != nil {
		return nil, Payload{}, s.restore(id, err)
	}
	if _, err := s.Forms.Transition(id, LocalPickerKind, userID, "processing", "resolved", messageID); err != nil {
		if _, cancelErr := s.Cancel(req.ID, userID); cancelErr != nil {
			return nil, Payload{}, fmt.Errorf("%w; cancel prepared upgrade: %v", err, cancelErr)
		}
		return nil, Payload{}, err
	}
	return req, payload, nil
}

func (s Service) Confirm(ctx context.Context, id, userID, messageID string) (Payload, string, error) {
	req, err := s.Forms.Transition(id, PendingKind, userID, "pending", "processing", messageID)
	if err != nil {
		return Payload{}, "", err
	}
	var payload Payload
	if err := json.Unmarshal([]byte(req.PayloadJSON), &payload); err != nil {
		return Payload{}, "", s.restore(id, fmt.Errorf("升级参数损坏: %w", err))
	}
	env, err := s.environment(ctx, true)
	if err != nil {
		return Payload{}, "", s.restore(id, err)
	}
	payload.UnitName = "feidex-upgrade-" + id
	payload.LaunchStartedAt = time.Now().Unix()
	payload.ChatID, payload.FeishuMsgID = req.SessionKey, textutil.FirstNonEmpty(messageID, req.FeishuMsgID)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Payload{}, "", s.restore(id, err)
	}
	if err := s.Forms.Repository.UpdatePending(id, func(current *interaction.PendingRequest) {
		if current.Status == "processing" {
			current.Status = "launching"
			current.PayloadJSON = string(encoded)
		}
	}); err != nil {
		return Payload{}, "", s.restore(id, err)
	}
	unit, err := s.Launcher.Launch(ctx, LaunchSpec{OperationID: id, UnitName: payload.UnitName, ServiceName: env.ServiceName, Version: textutil.FirstNonEmpty(payload.TargetVersion, payload.SourceName, "local-artifact"), BinaryPath: payload.BinaryPath, DownloadURL: payload.DownloadURL, SourcePath: payload.SourcePath, ExpectedSHA256: payload.ExpectedSHA256})
	if err != nil {
		if saveErr := s.Forms.Repository.UpdatePending(id, func(current *interaction.PendingRequest) {
			if current.Status == "launching" && current.PayloadJSON == string(encoded) {
				current.Status = "pending"
			}
		}); saveErr != nil {
			return Payload{}, "", fmt.Errorf("%w; restore upgrade request: %v", err, saveErr)
		}
		return Payload{}, "", err
	}
	if unit != payload.UnitName {
		return Payload{}, "", fmt.Errorf("upgrade launcher changed operation identity: %q", unit)
	}
	if err := s.Forms.Repository.UpdatePending(id, func(current *interaction.PendingRequest) {
		if current.Status == "launching" && current.PayloadJSON == string(encoded) {
			current.Status = "upgrading"
		}
	}); err != nil {
		return Payload{}, "", err
	}
	return payload, req.SessionKey, nil
}

func (s Service) restore(id string, cause error) error {
	if err := s.Forms.SaveDraft(id, nil, "pending", 0, ""); err != nil {
		return fmt.Errorf("%w; restore upgrade request: %v", cause, err)
	}
	return cause
}
