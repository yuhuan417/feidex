package app

import (
	"context"
	"encoding/json"
	"errors"
	"feidex/internal/domain/conversation"
	frontendruntime "feidex/internal/runtime"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appfeishuwrap "feidex/internal/app/feishuwrap"
	"feidex/internal/app/turnbinding"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/release"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// downloadFeishuStub records the message IDs it was asked to download and
// returns a fixed local path for each.
type downloadFeishuStub struct {
	*fakeFeishuClient
	downloadPath string
	messageIDs   []string
}

func (s *downloadFeishuStub) DownloadMessageResource(_ context.Context, messageID string, _ feishu.Attachment, _ string) (string, string, error) {
	s.messageIDs = append(s.messageIDs, messageID)
	return s.downloadPath, filepath.Base(s.downloadPath), nil
}

type fakeCodexClient struct {
	mu             sync.Mutex
	startErr       error
	closeErr       error
	callErr        error
	replyErr       error
	started        bool
	closed         bool
	startHook      func(context.Context, bool) error
	replies        []fakeCodexReply
	replyErrors    []fakeCodexReplyError
	callHook       func(context.Context, string, any, any) error
	onNotification func(string, json.RawMessage)
	onRequest      func(codexrpc.RequestEnvelope)
	onError        func(error)
}

type fakeCodexReply struct {
	id     json.RawMessage
	result any
}

type fakeCodexReplyError struct {
	id   json.RawMessage
	code int
	msg  string
}

type fakeReleaseClient struct {
	info         *release.ReleaseInfo
	devInfo      *release.ReleaseInfo
	versionInfo  map[string]*release.ReleaseInfo
	err          error
	devErr       error
	latestErr    error
	versionErr   error
	latestCalls  int
	devCalls     int
	versionCalls []string
}

type blockingReleaseClient struct {
	started chan struct{}
	release chan struct{}
	info    *release.ReleaseInfo
}

func (f *fakeReleaseClient) LatestLinuxBinary(context.Context, string) (*release.ReleaseInfo, error) {
	f.latestCalls++
	if f.latestErr != nil {
		return nil, f.latestErr
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.info == nil {
		return nil, errors.New("missing release info")
	}
	cp := *f.info
	return &cp, nil
}

func (f *blockingReleaseClient) LatestLinuxBinary(context.Context, string) (*release.ReleaseInfo, error) {
	close(f.started)
	<-f.release
	cp := *f.info
	return &cp, nil
}

func (f *blockingReleaseClient) LatestDevLinuxBinary(context.Context, string) (*release.ReleaseInfo, error) {
	close(f.started)
	<-f.release
	cp := *f.info
	return &cp, nil
}

func (f *blockingReleaseClient) LinuxBinaryByVersion(context.Context, string, string) (*release.ReleaseInfo, error) {
	close(f.started)
	<-f.release
	cp := *f.info
	return &cp, nil
}

func (f *fakeReleaseClient) LatestDevLinuxBinary(context.Context, string) (*release.ReleaseInfo, error) {
	f.devCalls++
	if f.devErr != nil {
		return nil, f.devErr
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.devInfo != nil {
		cp := *f.devInfo
		return &cp, nil
	}
	if info := f.versionInfo[release.DevReleaseTag]; info != nil {
		cp := *info
		return &cp, nil
	}
	if f.info == nil {
		return nil, errors.New("missing release info")
	}
	cp := *f.info
	return &cp, nil
}

func (f *fakeReleaseClient) LinuxBinaryByVersion(_ context.Context, version string, _ string) (*release.ReleaseInfo, error) {
	f.versionCalls = append(f.versionCalls, version)
	if f.versionErr != nil {
		return nil, f.versionErr
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.versionInfo != nil {
		if info := f.versionInfo[version]; info != nil {
			cp := *info
			return &cp, nil
		}
	}
	if f.info == nil {
		return nil, errors.New("missing release info")
	}
	cp := *f.info
	return &cp, nil
}

type fakeDaemonManagerForApp struct {
	status *daemon.Status
	err    error
}

func (f *fakeDaemonManagerForApp) Install(daemon.Config) error { return nil }
func (f *fakeDaemonManagerForApp) Uninstall() error            { return nil }
func (f *fakeDaemonManagerForApp) Start() error                { return nil }
func (f *fakeDaemonManagerForApp) Stop() error                 { return nil }
func (f *fakeDaemonManagerForApp) Restart() error              { return nil }
func (f *fakeDaemonManagerForApp) Status() (*daemon.Status, error) {
	return f.status, f.err
}
func (f *fakeDaemonManagerForApp) Platform() string { return "test" }
func (f *fakeDaemonManagerForApp) LogFile() string  { return "" }

func (f *fakeCodexClient) SetHandlers(onNotification func(string, json.RawMessage), onRequest func(codexrpc.RequestEnvelope)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onNotification = onNotification
	f.onRequest = onRequest
}

func (f *fakeCodexClient) SetErrorHandler(onError func(error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onError = onError
}

func (f *fakeCodexClient) Start(ctx context.Context, experimentalAPI bool) error {
	f.mu.Lock()
	f.started = true
	hook := f.startHook
	err := f.startErr
	f.mu.Unlock()
	if hook != nil {
		return hook(ctx, experimentalAPI)
	}
	return err
}

func (f *fakeCodexClient) Close() error {
	f.mu.Lock()
	f.closed = true
	err := f.closeErr
	f.mu.Unlock()
	return err
}

func (f *fakeCodexClient) Call(ctx context.Context, method string, params any, out any) error {
	f.mu.Lock()
	hook := f.callHook
	err := f.callErr
	f.mu.Unlock()
	if hook != nil {
		return hook(ctx, method, params, out)
	}
	return err
}

func (f *fakeCodexClient) Reply(id json.RawMessage, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.replyErr != nil {
		return f.replyErr
	}
	f.replies = append(f.replies, fakeCodexReply{
		id:     append(json.RawMessage(nil), id...),
		result: result,
	})
	return nil
}

func (f *fakeCodexClient) ReplyError(id json.RawMessage, code int, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyErrors = append(f.replyErrors, fakeCodexReplyError{
		id:   append(json.RawMessage(nil), id...),
		code: code,
		msg:  msg,
	})
	return nil
}

func (f *fakeCodexClient) handlersSnapshot() (func(string, json.RawMessage), func(codexrpc.RequestEnvelope), func(error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.onNotification, f.onRequest, f.onError
}

func (f *fakeCodexClient) statusSnapshot() (started bool, closed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started, f.closed
}

type fakeFeishuClient struct {
	mu                        sync.Mutex
	startErr                  error
	replyTextErr              error
	sendTextErr               error
	replyCardErr              error
	sendCardErr               error
	patchCardErr              error
	replyLocalAttachmentErr   error
	replyLocalImageErr        error
	replyLocalVideoErr        error
	rewriteLocalFileLinksErr  error
	addReactionErr            error
	removeReactionErr         error
	downloadErr               error
	shareFileErr              error
	urgentAppErr              error
	lookupMessageSenderErr    error
	getGroupBotCountErr       error
	cleanupResult             feishu.PreviewDriveCleanupResult
	cleanupErr                error
	cleanupHook               func(context.Context, time.Time) (feishu.PreviewDriveCleanupResult, error)
	started                   bool
	stopped                   bool
	replyTexts                []string
	sentTexts                 []string
	replyCards                []map[string]any
	sendCards                 []map[string]any
	sendCardChatIDs           []string
	patchedCards              []map[string]any
	replyCardInThread         []bool
	replyTextWithIDs          []string
	replyLocalAttachmentCalls []string
	replyLocalImageCalls      []string
	replyLocalVideoCalls      []string
	replyCardID               string
	replyCardIDs              []string
	replyTextIDs              []string
	sendCardID                string
	sendCardIDs               []string
	localFileLinkStatePath    string
	localFileLinkProcessCWD   string
	rewriteLocalFileLinksOut  string
	rewriteLocalFileLinkReqs  []feishu.LocalFileLinkRewriteRequest
	rewriteLocalFileLinksHook func(context.Context, feishu.LocalFileLinkRewriteRequest) (string, error)
	downloadPath              string
	downloadName              string
	mergeForwardText          string
	mergeForwardAttachments   []feishu.Attachment
	mergeForwardErr           error
	resolveMergeForwardHook   func(context.Context, string, []string) (string, []feishu.Attachment, error)
	mergeForwardCalls         []struct {
		messageID string
		ids       []string
	}
	sharedFileResult         feishu.SharedFileResult
	sharedFileRequests       []feishu.SharedFileRequest
	urgentAppCalls           []struct{ messageID, userID string }
	lookupMessageSenderCalls []string
	lookupMessageSenderOpen  string
	botOpenID                string
	botName                  string
	groupBotCounts           map[string]int
	groupBotCountCalls       []string
	announcementBlocks       []feishu.AnnouncementBlock
	announcementListErr      error
	announcementCreateErr    error
	announcementUpdateErr    error
	announcementListCalls    []string
	announcementCreateCalls  []fakeAnnouncementCreateCall
	announcementUpdateCalls  []struct{ chatID, blockID, content, clientToken string }
	onMessage                func(*feishu.InboundMessage)
	onBotAdded               func(*feishu.BotGroupEvent)
}

type fakeAnnouncementCreateCall struct {
	chatID        string
	parentBlockID string
	content       string
	clientToken   string
	index         *int
}

func (f *fakeFeishuClient) SetHandlers(onMessage func(*feishu.InboundMessage), _ func(*feishu.CardAction) (*callback.CardActionTriggerResponse, error), _ func(*feishu.MessageRecall), _ func(*feishu.MessageReaction)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onMessage = onMessage
}

func (f *fakeFeishuClient) SetBotGroupAddedHandler(handler func(*feishu.BotGroupEvent)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onBotAdded = handler
}

func (f *fakeFeishuClient) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = true
	return f.startErr
}

func (f *fakeFeishuClient) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
}

func (f *fakeFeishuClient) ConfigureLocalFileLinks(statePath, processCWD string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.localFileLinkStatePath = statePath
	f.localFileLinkProcessCWD = processCWD
}

func (f *fakeFeishuClient) RewriteLocalFileLinks(ctx context.Context, req feishu.LocalFileLinkRewriteRequest) (string, error) {
	f.mu.Lock()
	f.rewriteLocalFileLinkReqs = append(f.rewriteLocalFileLinkReqs, req)
	hook := f.rewriteLocalFileLinksHook
	out := f.rewriteLocalFileLinksOut
	err := f.rewriteLocalFileLinksErr
	f.mu.Unlock()
	if hook != nil {
		return hook(ctx, req)
	}
	return out, err
}

func (f *fakeFeishuClient) CleanupArtifactsBefore(ctx context.Context, cutoff time.Time) (feishu.PreviewDriveCleanupResult, error) {
	f.mu.Lock()
	hook := f.cleanupHook
	result := f.cleanupResult
	err := f.cleanupErr
	f.mu.Unlock()
	if hook != nil {
		return hook(ctx, cutoff)
	}
	return result, err
}

func (f *fakeFeishuClient) AddReaction(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addReactionErr
}
func (f *fakeFeishuClient) RemoveReaction(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.removeReactionErr
}

func (f *fakeFeishuClient) ReplyText(_ context.Context, _ string, text string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyTexts = append(f.replyTexts, text)
	return f.replyTextErr
}

func (f *fakeFeishuClient) ReplyTextWithID(_ context.Context, _ string, text string, _ bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyTextWithIDs = append(f.replyTextWithIDs, text)
	if len(f.replyTextIDs) > 0 {
		id := f.replyTextIDs[0]
		f.replyTextIDs = f.replyTextIDs[1:]
		return id, nil
	}
	return "reply-text-id", nil
}

func (f *fakeFeishuClient) SendText(_ context.Context, _ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentTexts = append(f.sentTexts, text)
	return f.sendTextErr
}

func (f *fakeFeishuClient) ReplyCard(_ context.Context, _ string, card map[string]any, inThread bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyCards = append(f.replyCards, cloneTestCard(card))
	f.replyCardInThread = append(f.replyCardInThread, inThread)
	if len(f.replyCardIDs) > 0 {
		id := f.replyCardIDs[0]
		f.replyCardIDs = f.replyCardIDs[1:]
		return id, f.replyCardErr
	}
	if f.replyCardID == "" {
		f.replyCardID = "reply-card-id"
	}
	return f.replyCardID, f.replyCardErr
}

func (f *fakeFeishuClient) SendCard(_ context.Context, chatID string, card map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendCardChatIDs = append(f.sendCardChatIDs, chatID)
	f.sendCards = append(f.sendCards, cloneTestCard(card))
	if len(f.sendCardIDs) > 0 {
		id := f.sendCardIDs[0]
		f.sendCardIDs = f.sendCardIDs[1:]
		return id, f.sendCardErr
	}
	if f.sendCardID == "" {
		f.sendCardID = "send-card-id"
	}
	return f.sendCardID, f.sendCardErr
}

func (f *fakeFeishuClient) PatchCard(_ context.Context, _ string, card map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patchedCards = append(f.patchedCards, cloneTestCard(card))
	return f.patchCardErr
}

func (f *fakeFeishuClient) ReplyLocalAttachment(_ context.Context, _ string, path string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyLocalAttachmentCalls = append(f.replyLocalAttachmentCalls, path)
	return f.replyLocalAttachmentErr
}

func (f *fakeFeishuClient) ReplyLocalImage(_ context.Context, _ string, path string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyLocalImageCalls = append(f.replyLocalImageCalls, path)
	return f.replyLocalImageErr
}

func (f *fakeFeishuClient) ReplyLocalVideo(_ context.Context, _ string, path string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyLocalVideoCalls = append(f.replyLocalVideoCalls, path)
	return f.replyLocalVideoErr
}

func (f *fakeFeishuClient) DownloadMessageResource(context.Context, string, feishu.Attachment, string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.downloadPath, f.downloadName, f.downloadErr
}

func (f *fakeFeishuClient) ResolveMergeForward(ctx context.Context, messageID string, messageIDs []string) (string, []feishu.Attachment, error) {
	ids := append([]string(nil), messageIDs...)
	f.mu.Lock()
	f.mergeForwardCalls = append(f.mergeForwardCalls, struct {
		messageID string
		ids       []string
	}{messageID: messageID, ids: ids})
	hook := f.resolveMergeForwardHook
	attachments := append([]feishu.Attachment(nil), f.mergeForwardAttachments...)
	text := f.mergeForwardText
	err := f.mergeForwardErr
	f.mu.Unlock()
	if hook != nil {
		return hook(ctx, messageID, ids)
	}
	return text, attachments, err
}

func (f *fakeFeishuClient) ShareLocalFile(_ context.Context, req feishu.SharedFileRequest) (feishu.SharedFileResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sharedFileRequests = append(f.sharedFileRequests, req)
	return f.sharedFileResult, f.shareFileErr
}

func (f *fakeFeishuClient) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	return (&feishu.Adapter{}).SimpleStatusCard(title, color, body, buttons)
}

func (f *fakeFeishuClient) UrgentApp(_ context.Context, messageID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urgentAppCalls = append(f.urgentAppCalls, struct{ messageID, userID string }{messageID: messageID, userID: userID})
	return f.urgentAppErr
}

func (f *fakeFeishuClient) LookupMessageSenderOpenID(_ context.Context, messageID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookupMessageSenderCalls = append(f.lookupMessageSenderCalls, messageID)
	return f.lookupMessageSenderOpen, f.lookupMessageSenderErr
}

func (f *fakeFeishuClient) GetGroupBotCount(_ context.Context, chatID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groupBotCountCalls = append(f.groupBotCountCalls, chatID)
	if f.getGroupBotCountErr != nil {
		return 0, f.getGroupBotCountErr
	}
	if f.groupBotCounts != nil {
		return f.groupBotCounts[chatID], nil
	}
	return 0, errors.New("group bot count not configured")
}

func (f *fakeFeishuClient) ListAnnouncementBlocks(_ context.Context, chatID string) ([]feishu.AnnouncementBlock, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.announcementListCalls = append(f.announcementListCalls, chatID)
	if f.announcementListErr != nil {
		return nil, f.announcementListErr
	}
	out := make([]feishu.AnnouncementBlock, len(f.announcementBlocks))
	copy(out, f.announcementBlocks)
	return out, nil
}

func (f *fakeFeishuClient) CreateAnnouncementTextBlock(_ context.Context, chatID, parentBlockID, content, clientToken string) (feishu.AnnouncementBlock, error) {
	return f.createAnnouncementTextBlock(chatID, parentBlockID, content, clientToken, nil)
}

func (f *fakeFeishuClient) CreateAnnouncementTextBlockAt(_ context.Context, chatID, parentBlockID, content, clientToken string, index int) (feishu.AnnouncementBlock, error) {
	return f.createAnnouncementTextBlock(chatID, parentBlockID, content, clientToken, &index)
}

func (f *fakeFeishuClient) createAnnouncementTextBlock(chatID, parentBlockID, content, clientToken string, index *int) (feishu.AnnouncementBlock, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var indexCopy *int
	if index != nil {
		v := *index
		indexCopy = &v
	}
	f.announcementCreateCalls = append(f.announcementCreateCalls, fakeAnnouncementCreateCall{chatID: chatID, parentBlockID: parentBlockID, content: content, clientToken: clientToken, index: indexCopy})
	if f.announcementCreateErr != nil {
		return feishu.AnnouncementBlock{}, f.announcementCreateErr
	}
	blockID := "announcement-block-created"
	if len(f.announcementBlocks) > 0 {
		blockID = blockID + "-next"
	}
	block := feishu.AnnouncementBlock{BlockID: blockID, Text: content}
	if index == nil || *index >= len(f.announcementBlocks) {
		f.announcementBlocks = append(f.announcementBlocks, block)
	} else {
		insertAt := *index
		if insertAt < 0 {
			insertAt = 0
		}
		f.announcementBlocks = append(f.announcementBlocks, feishu.AnnouncementBlock{})
		copy(f.announcementBlocks[insertAt+1:], f.announcementBlocks[insertAt:])
		f.announcementBlocks[insertAt] = block
	}
	return block, nil
}

func (f *fakeFeishuClient) UpdateAnnouncementTextBlock(_ context.Context, chatID, blockID, content, clientToken string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.announcementUpdateCalls = append(f.announcementUpdateCalls, struct{ chatID, blockID, content, clientToken string }{chatID: chatID, blockID: blockID, content: content, clientToken: clientToken})
	if f.announcementUpdateErr != nil {
		return f.announcementUpdateErr
	}
	for i := range f.announcementBlocks {
		if f.announcementBlocks[i].BlockID == blockID {
			f.announcementBlocks[i].Text = content
			return nil
		}
	}
	f.announcementBlocks = append(f.announcementBlocks, feishu.AnnouncementBlock{BlockID: blockID, Text: content})
	return nil
}

func (f *fakeFeishuClient) BotOpenID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.TrimSpace(f.botOpenID)
}

func (f *fakeFeishuClient) BotName() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.TrimSpace(f.botName)
}

func (f *fakeFeishuClient) replyCardsSnapshot() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneTestCardSlice(f.replyCards)
}

func (f *fakeFeishuClient) patchedCardsSnapshot() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneTestCardSlice(f.patchedCards)
}

func (f *fakeFeishuClient) replyTextsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.replyTexts...)
}

func (f *fakeFeishuClient) sharedFileRequestsSnapshot() []feishu.SharedFileRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]feishu.SharedFileRequest(nil), f.sharedFileRequests...)
}

func (f *fakeFeishuClient) setCleanupState(result feishu.PreviewDriveCleanupResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanupResult = result
	f.cleanupErr = err
}

func (f *fakeFeishuClient) setCleanupHook(hook func(context.Context, time.Time) (feishu.PreviewDriveCleanupResult, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanupHook = hook
}

func cloneTestCardSlice(cards []map[string]any) []map[string]any {
	if len(cards) == 0 {
		return nil
	}
	out := make([]map[string]any, len(cards))
	for i, card := range cards {
		out[i] = cloneTestCard(card)
	}
	return out
}

func cloneTestCard(card map[string]any) map[string]any {
	if card == nil {
		return nil
	}
	cloned, _ := cloneTestValue(card).(map[string]any)
	return cloned
}

func cloneTestValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for key, item := range current {
			out[key] = cloneTestValue(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(current))
		for i, item := range current {
			out[i] = cloneTestCard(item)
		}
		return out
	case []any:
		out := make([]any, len(current))
		for i, item := range current {
			out[i] = cloneTestValue(item)
		}
		return out
	case []string:
		return append([]string(nil), current...)
	case []feishu.Button:
		return append([]feishu.Button(nil), current...)
	case []feishu.Attachment:
		return append([]feishu.Attachment(nil), current...)
	case []feishu.SharedFileRequest:
		return append([]feishu.SharedFileRequest(nil), current...)
	case map[string]string:
		out := make(map[string]string, len(current))
		for key, item := range current {
			out[key] = item
		}
		return out
	default:
		return current
	}
}

func cardMarkdownContent(t *testing.T, card map[string]any) string {
	t.Helper()
	elements := cardElementsForTest(card)
	if len(elements) == 0 {
		t.Fatalf("unexpected card elements: %#v", card)
	}
	var parts []string
	for _, elem := range elements {
		if content, ok := elem["content"].(string); ok {
			parts = append(parts, content)
			continue
		}
		if text, ok := elem["text"].(map[string]any); ok {
			if content, ok := text["content"].(string); ok {
				parts = append(parts, content)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func cardElementsForTest(card map[string]any) []map[string]any {
	if elements, ok := card["elements"].([]map[string]any); ok {
		return elements
	}
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	return elements
}

func cardButtonsForTest(card map[string]any) []map[string]any {
	var buttons []map[string]any
	for _, elem := range cardElementsForTest(card) {
		if actions, ok := elem["actions"].([]map[string]any); ok {
			buttons = append(buttons, actions...)
		}
		if columns, ok := elem["columns"].([]map[string]any); ok {
			for _, column := range columns {
				columnElems, _ := column["elements"].([]map[string]any)
				for _, child := range columnElems {
					if tag, _ := child["tag"].(string); tag == "button" {
						buttons = append(buttons, child)
					}
				}
			}
		}
	}
	return buttons
}

func cardButtonLabelsByAction(card map[string]any) map[string]string {
	labels := map[string]string{}
	for _, action := range cardButtonsForTest(card) {
		text, _ := action["text"].(map[string]any)
		label, _ := text["content"].(string)
		value, _ := action["value"].(map[string]any)
		if len(value) == 0 {
			behaviors, _ := action["behaviors"].([]map[string]any)
			if len(behaviors) > 0 {
				value, _ = behaviors[0]["value"].(map[string]any)
			}
		}
		actionName, _ := value["action"].(string)
		if actionName != "" {
			labels[actionName] = label
		}
	}
	return labels
}

func firstCardActionValueForTest(card map[string]any, actionName string) map[string]any {
	for _, button := range cardButtonsForTest(card) {
		value, _ := button["value"].(map[string]any)
		if len(value) == 0 {
			behaviors, _ := button["behaviors"].([]map[string]any)
			if len(behaviors) > 0 {
				value, _ = behaviors[0]["value"].(map[string]any)
			}
		}
		if got, _ := value["action"].(string); got == actionName {
			return value
		}
	}
	return nil
}

func firstCardSelectActionValueForTest(card map[string]any, name string) map[string]any {
	for _, selectStatic := range cardSelectStaticForTest(card) {
		if got, _ := selectStatic["name"].(string); got != name {
			continue
		}
		behaviors, _ := selectStatic["behaviors"].([]map[string]any)
		if len(behaviors) == 0 {
			return nil
		}
		value, _ := behaviors[0]["value"].(map[string]any)
		return value
	}
	return nil
}

func cardSelectStaticForTest(card map[string]any) []map[string]any {
	var selects []map[string]any
	for _, elem := range cardElementsForTest(card) {
		if tag, _ := elem["tag"].(string); tag == "select_static" {
			selects = append(selects, elem)
		}
	}
	return selects
}

func newTestApp(t *testing.T) (*App, *fakeFeishuClient, *fakeCodexClient) {
	t.Helper()

	cfg := config.Default()
	cfg.Feishu.Backend = backendCodex
	cfg.Workspaces[0].Cwd = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save(config) error = %v", err)
	}
	loadedCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(config) error = %v", err)
	}
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	ff := &fakeFeishuClient{botOpenID: "bot-open"}
	fc := &fakeCodexClient{}
	var asyncWG sync.WaitGroup
	a := &App{
		cfg:     loadedCfg,
		cfgPath: cfgPath,
		store:   store,
		codex:   fc,
		feishu:  appfeishuwrap.WrapFeishuClient(ff),
		started: time.Now(),
		asyncRunner: func(fn func()) {
			asyncWG.Add(1)
			go func() {
				defer asyncWG.Done()
				fn()
			}()
		},
		waitAsync:   asyncWG.Wait,
		liveThreads: frontendruntime.NewLiveThreads(),
		trackers: appTrackers{
			turnStreams:  newTurnStreamTracker(),
			turnBindings: turnbinding.NewTracker(store),
		},
	}
	replaceCodexClient(a, fc)
	configureGroupPrimaryEvents(a)
	t.Cleanup(asyncWG.Wait)
	return a, ff, fc
}

func seedActiveSubmission(t *testing.T, a *App, sessionKey, threadID, turnID string) *state.Submission {
	t.Helper()

	if err := a.store.UpsertSession(&conversation.Session{
		Key:                sessionKey,
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ActiveThreadID:     threadID,
		ActiveTurnID:       turnID,
		ActiveSubmissionID: "sub-1",
		OwnerUserID:        "user-1",
		ChatID:             "chat-1",
		ChatType:           "group",
		Status:             "running",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-1",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		ThreadID:         threadID,
		TurnID:           turnID,
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "trigger-1",
		Status:           "running",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	return a.store.GetSubmission(subID)
}

func newTestClaudeRuntime(t *testing.T, a *App) *claudeRuntime {
	t.Helper()
	rc := newClaudeRuntime(a, a.cfg.Claude)
	rt, ok := rc.(*claudeRuntime)
	if !ok {
		t.Fatalf("newTestClaudeRuntime: expected *claudeRuntime, got %T", rc)
	}
	return rt
}
