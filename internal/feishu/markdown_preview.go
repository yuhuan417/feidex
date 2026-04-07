package feishu

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
)

const (
	defaultPreviewRootFolderName = "Feidex Markdown Previews"
	defaultPreviewMaxFileBytes   = 20 * 1024 * 1024
	previewFileType              = "file"
	previewFolderType            = "folder"
	previewPermissionView        = "view"
)

var markdownPreviewLinkRe = regexp.MustCompile(`\[[^\]]+\]\(([^)\n]+)\)`)
var markdownPreviewLineSuffixRe = regexp.MustCompile(`^(.*\.md)(:\d+(?::\d+)?)$`)

type MarkdownPreviewRequest struct {
	Text         string
	WorkspaceCWD string
	ChatID       string
	UserID       string
}

type MarkdownPreviewConfig struct {
	StatePath      string
	RootFolderName string
	ProcessCWD     string
	MaxFileBytes   int64
}

type PreviewDriveCleanupResult struct {
	DeletedFileCount      int   `json:"deleted_file_count"`
	DeletedEstimatedBytes int64 `json:"deleted_estimated_bytes"`
}

type DriveMarkdownPreviewer struct {
	api    previewDriveAPI
	config MarkdownPreviewConfig

	mu     sync.Mutex
	loaded bool
	state  *previewState
}

type previewDriveAPI interface {
	CreateFolder(context.Context, string, string) (previewRemoteNode, error)
	UploadFile(context.Context, string, string, []byte) (string, error)
	QueryMetaURL(context.Context, string, string) (string, error)
	GrantPermission(context.Context, string, string, previewPrincipal) error
	DeleteFile(context.Context, string, string) error
}

type previewRemoteNode struct {
	Token string
	URL   string
	Type  string
}

type previewPrincipal struct {
	Key        string
	MemberType string
	MemberID   string
	Type       string
}

type previewState struct {
	Root  *previewFolderRecord          `json:"root,omitempty"`
	Files map[string]*previewFileRecord `json:"files,omitempty"`
}

type previewFolderRecord struct {
	Token string `json:"token,omitempty"`
	URL   string `json:"url,omitempty"`
}

type previewFileRecord struct {
	Path       string          `json:"path,omitempty"`
	SHA256     string          `json:"sha256,omitempty"`
	Token      string          `json:"token,omitempty"`
	URL        string          `json:"url,omitempty"`
	Shared     map[string]bool `json:"shared,omitempty"`
	SizeBytes  int64           `json:"size_bytes,omitempty"`
	CreatedAt  time.Time       `json:"created_at,omitempty"`
	LastUsedAt time.Time       `json:"last_used_at,omitempty"`
}

type driveAPIError struct {
	Code int
	Msg  string
}

func (e *driveAPIError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Msg) == "" {
		return fmt.Sprintf("feishu drive api error %d", e.Code)
	}
	return fmt.Sprintf("feishu drive api error %d: %s", e.Code, strings.TrimSpace(e.Msg))
}

func NewDriveMarkdownPreviewer(api previewDriveAPI, cfg MarkdownPreviewConfig) *DriveMarkdownPreviewer {
	if cfg.RootFolderName == "" {
		cfg.RootFolderName = defaultPreviewRootFolderName
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = defaultPreviewMaxFileBytes
	}
	if cfg.ProcessCWD == "" {
		if cwd, err := os.Getwd(); err == nil {
			cfg.ProcessCWD = cwd
		}
	}
	return &DriveMarkdownPreviewer{
		api:    api,
		config: cfg,
	}
}

func (a *Adapter) RewriteMarkdownPreview(ctx context.Context, req MarkdownPreviewRequest) (string, error) {
	previewer := a.ensureMarkdownPreviewer()
	if previewer == nil {
		return req.Text, nil
	}
	return previewer.RewriteText(ctx, req)
}

func (a *Adapter) CleanupMarkdownPreviewsBefore(ctx context.Context, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	previewer := a.ensureMarkdownPreviewer()
	if previewer == nil {
		return PreviewDriveCleanupResult{}, nil
	}
	return previewer.CleanupBefore(ctx, cutoff)
}

func (a *Adapter) ensureMarkdownPreviewer() *DriveMarkdownPreviewer {
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	if strings.TrimSpace(a.previewStatePath) == "" {
		return nil
	}
	if a.previewer != nil {
		return a.previewer
	}
	a.previewer = NewDriveMarkdownPreviewer(NewLarkDrivePreviewAPI(a.client), MarkdownPreviewConfig{
		StatePath:      a.previewStatePath,
		ProcessCWD:     a.previewProcessCWD,
		RootFolderName: defaultPreviewRootFolderName,
	})
	return a.previewer
}

func (p *DriveMarkdownPreviewer) RewriteText(ctx context.Context, req MarkdownPreviewRequest) (string, error) {
	text := strings.TrimSpace(req.Text)
	if p == nil || p.api == nil || strings.TrimSpace(text) == "" {
		return req.Text, nil
	}
	principals := previewPrincipals(req.ChatID, req.UserID)
	if len(principals) == 0 {
		return req.Text, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.loadStateLocked()
	matches := markdownPreviewLinkRe.FindAllStringSubmatchIndex(req.Text, -1)
	if len(matches) == 0 {
		return req.Text, nil
	}

	rewrittenTargets := map[string]string{}
	var builder strings.Builder
	var errs []string
	last := 0
	changed := false
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		targetStart := match[2]
		targetEnd := match[3]
		rawTarget := req.Text[targetStart:targetEnd]
		builder.WriteString(req.Text[last:targetStart])
		replacement := rawTarget
		if cached, ok := rewrittenTargets[rawTarget]; ok {
			replacement = cached
			if replacement != rawTarget {
				changed = true
			}
		} else {
			url, ok, err := p.materializeMarkdownTargetLocked(ctx, state, rawTarget, req, principals)
			switch {
			case err != nil:
				errs = append(errs, err.Error())
			case ok && strings.TrimSpace(url) != "":
				replacement = url
				rewrittenTargets[rawTarget] = url
				changed = true
			default:
				rewrittenTargets[rawTarget] = rawTarget
			}
		}
		builder.WriteString(replacement)
		last = targetEnd
	}
	builder.WriteString(req.Text[last:])
	if changed {
		if err := p.saveStateLocked(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return builder.String(), fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return builder.String(), nil
}

func (p *DriveMarkdownPreviewer) CleanupBefore(ctx context.Context, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	if p == nil {
		return PreviewDriveCleanupResult{}, nil
	}
	if p.api == nil {
		return PreviewDriveCleanupResult{}, fmt.Errorf("preview drive api is not available")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.loadStateLocked()
	result := PreviewDriveCleanupResult{}
	keys := make([]string, 0, len(state.Files))
	for key := range state.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	changed := false
	for _, key := range keys {
		record := state.Files[key]
		if record == nil || record.LastUsedAt.IsZero() || !record.LastUsedAt.Before(cutoff) {
			continue
		}
		if strings.TrimSpace(record.Token) != "" {
			if err := p.api.DeleteFile(ctx, record.Token, previewFileType); err != nil {
				return result, err
			}
		}
		result.DeletedFileCount++
		result.DeletedEstimatedBytes += record.SizeBytes
		delete(state.Files, key)
		changed = true
	}
	if changed {
		if err := p.saveStateLocked(); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (p *DriveMarkdownPreviewer) materializeMarkdownTargetLocked(ctx context.Context, state *previewState, rawTarget string, req MarkdownPreviewRequest, principals []previewPrincipal) (string, bool, error) {
	resolvedPath, ok, err := p.resolveMarkdownPath(rawTarget, req)
	if err != nil || !ok {
		return "", ok, err
	}
	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", true, fmt.Errorf("read markdown preview source %s: %w", resolvedPath, err)
	}
	if len(content) == 0 {
		return "", true, fmt.Errorf("skip empty markdown preview source %s", resolvedPath)
	}
	if int64(len(content)) > p.config.MaxFileBytes {
		return "", true, fmt.Errorf("markdown preview source exceeds %d bytes: %s", p.config.MaxFileBytes, resolvedPath)
	}

	root, err := p.ensureRootFolderLocked(ctx, state)
	if err != nil {
		return "", true, err
	}

	sum := sha256.Sum256(content)
	contentSHA := hex.EncodeToString(sum[:])
	fileKey := previewFileKey(resolvedPath, contentSHA)
	record := state.Files[fileKey]
	if record == nil {
		record = &previewFileRecord{
			Path:      resolvedPath,
			SHA256:    contentSHA,
			SizeBytes: int64(len(content)),
			CreatedAt: time.Now().UTC(),
		}
		state.Files[fileKey] = record
	}
	record.Path = resolvedPath
	record.SHA256 = contentSHA
	record.SizeBytes = int64(len(content))
	record.LastUsedAt = time.Now().UTC()
	if record.Shared == nil {
		record.Shared = map[string]bool{}
	}
	if strings.TrimSpace(record.Token) == "" {
		token, err := p.api.UploadFile(ctx, root.Token, previewFileName(resolvedPath, contentSHA), content)
		if err != nil {
			return "", true, fmt.Errorf("upload markdown preview for %s: %w", resolvedPath, err)
		}
		record.Token = token
	}
	if strings.TrimSpace(record.URL) == "" {
		url, err := p.api.QueryMetaURL(ctx, record.Token, previewFileType)
		if err != nil {
			return "", true, fmt.Errorf("query markdown preview url for %s: %w", resolvedPath, err)
		}
		record.URL = url
	}
	if err := ensurePreviewPermissions(ctx, p.api, record.Token, previewFileType, record.Shared, principals); err != nil {
		return "", true, fmt.Errorf("authorize markdown preview for %s: %w", resolvedPath, err)
	}
	return record.URL, true, nil
}

func (p *DriveMarkdownPreviewer) ensureRootFolderLocked(ctx context.Context, state *previewState) (*previewFolderRecord, error) {
	if state.Root == nil {
		state.Root = &previewFolderRecord{}
	}
	if strings.TrimSpace(state.Root.Token) != "" {
		return state.Root, nil
	}
	node, err := p.api.CreateFolder(ctx, p.config.RootFolderName, "")
	if err != nil {
		return nil, fmt.Errorf("create markdown preview root folder: %w", err)
	}
	state.Root.Token = node.Token
	state.Root.URL = node.URL
	return state.Root, nil
}

func (p *DriveMarkdownPreviewer) resolveMarkdownPath(rawTarget string, req MarkdownPreviewRequest) (string, bool, error) {
	target := strings.TrimSpace(rawTarget)
	target = strings.Trim(target, "\"'")
	if target == "" {
		return "", false, nil
	}
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		target = strings.TrimPrefix(strings.TrimSuffix(target, ">"), "<")
	}
	if idx := strings.IndexByte(target, '#'); idx >= 0 {
		target = target[:idx]
	}
	if matched := markdownPreviewLineSuffixRe.FindStringSubmatch(target); len(matched) == 3 {
		target = matched[1]
	}
	target = filepath.Clean(strings.TrimSpace(target))
	if !strings.EqualFold(filepath.Ext(target), ".md") {
		return "", false, nil
	}

	roots := previewAllowedRoots(req.WorkspaceCWD, p.config.ProcessCWD)
	candidates := previewPathCandidates(target, roots)
	for _, candidate := range candidates {
		resolved, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if !previewPathWithinAnyRoot(resolved, roots) {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		return resolved, true, nil
	}
	return "", false, nil
}

func (p *DriveMarkdownPreviewer) loadStateLocked() *previewState {
	if p.loaded {
		return p.state
	}
	p.loaded = true
	p.state = &previewState{Files: map[string]*previewFileRecord{}}
	if strings.TrimSpace(p.config.StatePath) == "" {
		return p.state
	}
	data, err := os.ReadFile(p.config.StatePath)
	if err != nil {
		return p.state
	}
	var loaded previewState
	if err := json.Unmarshal(data, &loaded); err != nil {
		return p.state
	}
	p.state = normalizePreviewState(&loaded)
	return p.state
}

func (p *DriveMarkdownPreviewer) saveStateLocked() error {
	if strings.TrimSpace(p.config.StatePath) == "" {
		return nil
	}
	state := normalizePreviewState(p.state)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.config.StatePath), 0o755); err != nil {
		return err
	}
	tmp := p.config.StatePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.config.StatePath)
}

func normalizePreviewState(state *previewState) *previewState {
	if state == nil {
		return &previewState{Files: map[string]*previewFileRecord{}}
	}
	if state.Files == nil {
		state.Files = map[string]*previewFileRecord{}
	}
	for _, record := range state.Files {
		if record == nil {
			continue
		}
		if record.Shared == nil {
			record.Shared = map[string]bool{}
		}
	}
	return state
}

func ensurePreviewPermissions(ctx context.Context, api previewDriveAPI, token, docType string, shared map[string]bool, principals []previewPrincipal) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("missing preview token for %s", docType)
	}
	for _, principal := range principals {
		if principal.Key == "" || shared[principal.Key] {
			continue
		}
		if err := api.GrantPermission(ctx, token, docType, principal); err != nil {
			return err
		}
		shared[principal.Key] = true
	}
	return nil
}

func previewPrincipals(chatID, actorUserID string) []previewPrincipal {
	values := []previewPrincipal{}
	if userPrincipal, ok := previewUserPrincipal(actorUserID); ok {
		values = append(values, userPrincipal)
	}
	if chatPrincipal, ok := previewChatPrincipal(chatID); ok {
		seen := map[string]struct{}{}
		for _, principal := range values {
			seen[principal.Key] = struct{}{}
		}
		if _, exists := seen[chatPrincipal.Key]; !exists {
			values = append(values, chatPrincipal)
		}
	}
	return values
}

func previewUserPrincipal(actorUserID string) (previewPrincipal, bool) {
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return previewPrincipal{}, false
	}
	memberType := "userid"
	switch {
	case strings.HasPrefix(actorUserID, "ou_"):
		memberType = "openid"
	case strings.HasPrefix(actorUserID, "on_"):
		memberType = "unionid"
	}
	return previewPrincipal{
		Key:        memberType + ":" + actorUserID,
		MemberType: memberType,
		MemberID:   actorUserID,
		Type:       "user",
	}, true
}

func previewChatPrincipal(chatID string) (previewPrincipal, bool) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return previewPrincipal{}, false
	}
	return previewPrincipal{
		Key:        "openchat:" + chatID,
		MemberType: "openchat",
		MemberID:   chatID,
		Type:       "chat",
	}, true
}

func previewAllowedRoots(values ...string) []string {
	seen := map[string]struct{}{}
	roots := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		resolved, err := filepath.Abs(value)
		if err != nil {
			continue
		}
		resolved = filepath.Clean(resolved)
		if _, exists := seen[resolved]; exists {
			continue
		}
		seen[resolved] = struct{}{}
		roots = append(roots, resolved)
	}
	sort.Strings(roots)
	return roots
}

func previewPathCandidates(target string, roots []string) []string {
	if filepath.IsAbs(target) {
		return []string{target}
	}
	candidates := make([]string, 0, len(roots))
	for _, root := range roots {
		candidates = append(candidates, filepath.Join(root, target))
	}
	return candidates
}

func previewPathWithinAnyRoot(path string, roots []string) bool {
	if len(roots) == 0 {
		return false
	}
	path = filepath.Clean(path)
	for _, root := range roots {
		if previewPathWithinRoot(path, root) {
			return true
		}
	}
	return false
}

func previewPathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	if root == "." {
		return false
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

func previewFileKey(resolvedPath, contentSHA string) string {
	return resolvedPath + "|" + contentSHA
}

func previewFileName(resolvedPath, contentSHA string) string {
	base := strings.TrimSpace(filepath.Base(resolvedPath))
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		name = "preview"
	}
	name = sanitizePreviewFileComponent(name)
	shortSHA := contentSHA
	if len(shortSHA) > 12 {
		shortSHA = shortSHA[:12]
	}
	return name + "-" + shortSHA + ".md"
}

func sanitizePreviewFileComponent(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	trimmed := strings.Trim(b.String(), "-")
	if trimmed == "" {
		return "preview"
	}
	return trimmed
}

type larkDrivePreviewAPI struct {
	client *lark.Client
}

func NewLarkDrivePreviewAPI(client *lark.Client) previewDriveAPI {
	if client == nil {
		return nil
	}
	return &larkDrivePreviewAPI{client: client}
}

func (a *larkDrivePreviewAPI) CreateFolder(ctx context.Context, name, parentToken string) (previewRemoteNode, error) {
	resp, err := a.client.Drive.V1.File.CreateFolder(ctx, larkdrive.NewCreateFolderFileReqBuilder().
		Body(larkdrive.NewCreateFolderFileReqBodyBuilder().
			Name(name).
			FolderToken(parentToken).
			Build()).
		Build())
	if err != nil {
		return previewRemoteNode{}, err
	}
	if !resp.Success() {
		return previewRemoteNode{}, &driveAPIError{Code: resp.Code, Msg: resp.Msg}
	}
	if resp.Data == nil {
		return previewRemoteNode{}, fmt.Errorf("missing create folder response data")
	}
	return previewRemoteNode{
		Token: stringPtrValue(resp.Data.Token),
		URL:   stringPtrValue(resp.Data.Url),
		Type:  previewFolderType,
	}, nil
}

func (a *larkDrivePreviewAPI) UploadFile(ctx context.Context, parentToken, fileName string, content []byte) (string, error) {
	resp, err := a.client.Drive.V1.File.UploadAll(ctx, larkdrive.NewUploadAllFileReqBuilder().
		Body(larkdrive.NewUploadAllFileReqBodyBuilder().
			FileName(fileName).
			ParentType("explorer").
			ParentNode(parentToken).
			Size(len(content)).
			File(bytes.NewReader(content)).
			Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", &driveAPIError{Code: resp.Code, Msg: resp.Msg}
	}
	if resp.Data == nil {
		return "", fmt.Errorf("missing upload file response data")
	}
	return stringPtrValue(resp.Data.FileToken), nil
}

func (a *larkDrivePreviewAPI) QueryMetaURL(ctx context.Context, token, docType string) (string, error) {
	resp, err := a.client.Drive.V1.Meta.BatchQuery(ctx, larkdrive.NewBatchQueryMetaReqBuilder().
		MetaRequest(larkdrive.NewMetaRequestBuilder().
			RequestDocs([]*larkdrive.RequestDoc{
				larkdrive.NewRequestDocBuilder().
					DocToken(token).
					DocType(docType).
					Build(),
			}).
			WithUrl(true).
			Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", &driveAPIError{Code: resp.Code, Msg: resp.Msg}
	}
	if resp.Data == nil || len(resp.Data.Metas) == 0 || resp.Data.Metas[0] == nil {
		return "", fmt.Errorf("missing meta url for token %s", token)
	}
	return stringPtrValue(resp.Data.Metas[0].Url), nil
}

func (a *larkDrivePreviewAPI) GrantPermission(ctx context.Context, token, docType string, principal previewPrincipal) error {
	resp, err := a.client.Drive.V1.PermissionMember.Create(ctx, larkdrive.NewCreatePermissionMemberReqBuilder().
		Token(token).
		Type(docType).
		BaseMember(larkdrive.NewBaseMemberBuilder().
			MemberType(principal.MemberType).
			MemberId(principal.MemberID).
			Perm(previewPermissionView).
			Type(principal.Type).
			Build()).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() {
		return &driveAPIError{Code: resp.Code, Msg: resp.Msg}
	}
	return nil
}

func (a *larkDrivePreviewAPI) DeleteFile(ctx context.Context, token, docType string) error {
	resp, err := a.client.Drive.V1.File.Delete(ctx, larkdrive.NewDeleteFileReqBuilder().
		FileToken(token).
		Type(docType).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() {
		return &driveAPIError{Code: resp.Code, Msg: resp.Msg}
	}
	return nil
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
