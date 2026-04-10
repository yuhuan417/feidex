package feishu

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	previewManagedFilePrefix     = "fxmd-v2-"
	previewTimestampFormat       = "20060102T150405Z"
	previewListPageSize          = 200
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
	ListFiles(context.Context, string) ([]previewRemoteNode, error)
	UploadFile(context.Context, string, string, []byte) (string, error)
	QueryMetaURL(context.Context, string, string) (string, error)
	GrantPermission(context.Context, string, string, previewPrincipal) error
	DeleteFile(context.Context, string, string) error
}

type previewRemoteNode struct {
	Token string
	URL   string
	Type  string
	Name  string
}

type previewPrincipal struct {
	Key        string
	MemberType string
	MemberID   string
	Type       string
}

type previewState struct {
	Root *previewFolderRecord
}

type previewFolderRecord struct {
	Token string
	URL   string
}

type driveAPIError struct {
	Code  int
	Msg   string
	Issue *PermissionIssue
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

func (e *driveAPIError) PermissionIssue() *PermissionIssue {
	if e == nil {
		return nil
	}
	return e.Issue
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
	if a.client == nil {
		return nil
	}
	if a.previewer != nil {
		return a.previewer
	}
	a.previewer = NewDriveMarkdownPreviewer(NewLarkDrivePreviewAPI(a.client), MarkdownPreviewConfig{
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
			url, ok, err := p.materializeMarkdownTargetLocked(ctx, rawTarget, req, principals)
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
	if !changed {
		return req.Text, nil
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

	roots, err := p.listRootFoldersLocked(ctx)
	if err != nil {
		return PreviewDriveCleanupResult{}, err
	}
	result := PreviewDriveCleanupResult{}
	for _, root := range roots {
		if root == nil || strings.TrimSpace(root.Token) == "" {
			continue
		}
		nodes, err := p.api.ListFiles(ctx, root.Token)
		if err != nil {
			return result, err
		}
		for _, node := range nodes {
			if !strings.EqualFold(strings.TrimSpace(node.Type), previewFileType) {
				continue
			}
			createdAt, ok := previewManagedFileTime(node.Name)
			if !ok || !createdAt.Before(cutoff) {
				continue
			}
			if strings.TrimSpace(node.Token) != "" {
				if err := p.api.DeleteFile(ctx, node.Token, previewFileType); err != nil {
					return result, err
				}
			}
			result.DeletedFileCount++
		}
	}
	return result, nil
}

func (p *DriveMarkdownPreviewer) materializeMarkdownTargetLocked(ctx context.Context, rawTarget string, req MarkdownPreviewRequest, principals []previewPrincipal) (string, bool, error) {
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

	root, err := p.ensureRootFolderLocked(ctx)
	if err != nil {
		return "", true, err
	}

	sum := sha256.Sum256(content)
	contentSHA := hex.EncodeToString(sum[:])
	token, err := p.api.UploadFile(ctx, root.Token, previewFileName(resolvedPath, contentSHA, time.Now().UTC()), content)
	if err != nil {
		return "", true, fmt.Errorf("upload markdown preview for %s: %w", resolvedPath, err)
	}
	url, err := p.api.QueryMetaURL(ctx, token, previewFileType)
	if err != nil {
		return "", true, fmt.Errorf("query markdown preview url for %s: %w", resolvedPath, err)
	}
	if err := ensurePreviewPermissions(ctx, p.api, token, previewFileType, map[string]bool{}, principals); err != nil {
		return "", true, fmt.Errorf("authorize markdown preview for %s: %w", resolvedPath, err)
	}
	return url, true, nil
}

func (p *DriveMarkdownPreviewer) ensureRootFolderLocked(ctx context.Context) (*previewFolderRecord, error) {
	state := p.loadStateLocked()
	if state.Root != nil && strings.TrimSpace(state.Root.Token) != "" {
		return state.Root, nil
	}
	roots, err := p.listRootFoldersLocked(ctx)
	if err != nil {
		return nil, err
	}
	if len(roots) > 0 {
		state.Root = roots[0]
		return state.Root, nil
	}
	node, err := p.api.CreateFolder(ctx, p.config.RootFolderName, "")
	if err != nil {
		return nil, fmt.Errorf("create markdown preview root folder: %w", err)
	}
	state.Root = &previewFolderRecord{
		Token: node.Token,
		URL:   node.URL,
	}
	return state.Root, nil
}

func (p *DriveMarkdownPreviewer) listRootFoldersLocked(ctx context.Context) ([]*previewFolderRecord, error) {
	if p == nil || p.api == nil {
		return nil, nil
	}
	nodes, err := p.api.ListFiles(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]*previewFolderRecord, 0, len(nodes))
	for _, node := range nodes {
		if !strings.EqualFold(strings.TrimSpace(node.Type), previewFolderType) {
			continue
		}
		if strings.TrimSpace(node.Name) != strings.TrimSpace(p.config.RootFolderName) {
			continue
		}
		out = append(out, &previewFolderRecord{
			Token: node.Token,
			URL:   node.URL,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Token < out[j].Token
	})
	return out, nil
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
	p.state = &previewState{}
	return p.state
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

func previewFileName(resolvedPath, contentSHA string, now time.Time) string {
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
	return previewManagedFilePrefix + now.UTC().Format(previewTimestampFormat) + "-" + name + "-" + shortSHA + ".md"
}

func previewManagedFileTime(name string) (time.Time, bool) {
	return managedFileTime(previewManagedFilePrefix, name)
}

func managedFileTime(prefix, name string) (time.Time, bool) {
	prefix = strings.TrimSpace(prefix)
	name = strings.TrimSpace(name)
	if prefix == "" || !strings.HasPrefix(name, prefix) {
		return time.Time{}, false
	}
	rest := strings.TrimPrefix(name, prefix)
	if len(rest) < len(previewTimestampFormat)+1 || rest[len(previewTimestampFormat)] != '-' {
		return time.Time{}, false
	}
	parsed, err := time.Parse(previewTimestampFormat, rest[:len(previewTimestampFormat)])
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
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
		return previewRemoteNode{}, wrapPermissionIssue(err, permissionIssueFromDirectError("drive.file.create_folder", err))
	}
	if !resp.Success() {
		return previewRemoteNode{}, &driveAPIError{
			Code:  resp.Code,
			Msg:   resp.Msg,
			Issue: permissionIssueFromCodeError("drive.file.create_folder", resp.Code, resp.Msg, &resp.CodeError, resp.ApiResp, nil),
		}
	}
	if resp.Data == nil {
		return previewRemoteNode{}, fmt.Errorf("missing create folder response data")
	}
	return previewRemoteNode{
		Token: stringPtrValue(resp.Data.Token),
		URL:   stringPtrValue(resp.Data.Url),
		Type:  previewFolderType,
		Name:  name,
	}, nil
}

func (a *larkDrivePreviewAPI) ListFiles(ctx context.Context, folderToken string) ([]previewRemoteNode, error) {
	builder := larkdrive.NewListFileReqBuilder().
		PageSize(previewListPageSize).
		OrderBy(larkdrive.OrderByCreatedTime).
		Direction("DESC")
	if strings.TrimSpace(folderToken) != "" {
		builder.FolderToken(folderToken)
	}
	iterator, err := a.client.Drive.V1.File.ListByIterator(ctx, builder.Build())
	if err != nil {
		return nil, wrapPermissionIssue(err, permissionIssueFromDirectError("drive.file.list", err))
	}
	out := []previewRemoteNode{}
	for {
		ok, item, err := iterator.Next()
		if err != nil {
			return nil, wrapPermissionIssue(err, permissionIssueFromDirectError("drive.file.list", err))
		}
		if !ok {
			break
		}
		if item == nil {
			continue
		}
		out = append(out, previewRemoteNode{
			Token: stringPtrValue(item.Token),
			URL:   stringPtrValue(item.Url),
			Type:  stringPtrValue(item.Type),
			Name:  stringPtrValue(item.Name),
		})
	}
	return out, nil
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
		return "", wrapPermissionIssue(err, permissionIssueFromDirectError("drive.file.upload_all", err))
	}
	if !resp.Success() {
		return "", &driveAPIError{
			Code:  resp.Code,
			Msg:   resp.Msg,
			Issue: permissionIssueFromCodeError("drive.file.upload_all", resp.Code, resp.Msg, &resp.CodeError, resp.ApiResp, nil),
		}
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
		return "", wrapPermissionIssue(err, permissionIssueFromDirectError("drive.meta.batch_query", err))
	}
	if !resp.Success() {
		return "", &driveAPIError{
			Code:  resp.Code,
			Msg:   resp.Msg,
			Issue: permissionIssueFromCodeError("drive.meta.batch_query", resp.Code, resp.Msg, &resp.CodeError, resp.ApiResp, nil),
		}
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
		return wrapPermissionIssue(err, permissionIssueFromDirectError("drive.permission_member.create", err))
	}
	if !resp.Success() {
		return &driveAPIError{
			Code:  resp.Code,
			Msg:   resp.Msg,
			Issue: permissionIssueFromCodeError("drive.permission_member.create", resp.Code, resp.Msg, &resp.CodeError, resp.ApiResp, nil),
		}
	}
	return nil
}

func (a *larkDrivePreviewAPI) DeleteFile(ctx context.Context, token, docType string) error {
	resp, err := a.client.Drive.V1.File.Delete(ctx, larkdrive.NewDeleteFileReqBuilder().
		FileToken(token).
		Type(docType).
		Build())
	if err != nil {
		return wrapPermissionIssue(err, permissionIssueFromDirectError("drive.file.delete", err))
	}
	if !resp.Success() {
		return &driveAPIError{
			Code:  resp.Code,
			Msg:   resp.Msg,
			Issue: permissionIssueFromCodeError("drive.file.delete", resp.Code, resp.Msg, &resp.CodeError, resp.ApiResp, nil),
		}
	}
	return nil
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
