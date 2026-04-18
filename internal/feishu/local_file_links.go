package feishu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"feidex/internal/pathdisplay"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
)

const (
	defaultPreviewRootFolderName = defaultArtifactRootFolderName
	defaultPreviewMaxFileBytes   = 0
	previewFileType              = "file"
	previewFolderType            = "folder"
	previewPermissionView        = "view"
	previewManagedFilePrefix     = "fxmd-v2-"
	previewTimestampFormat       = "20060102T150405Z"
	previewListPageSize          = 200
)

var localFileLinkRe = regexp.MustCompile(`\[[^\]]+\]\(([^)\n]+)\)`)
var previewLineSuffixRe = regexp.MustCompile(`^(.*?)(:\d+(?::\d+)?)$`)

type LocalFileLinkRewriteRequest struct {
	Text         string
	WorkspaceCWD string
	ChatID       string
	UserID       string
}

type LocalFileLinkConfig struct {
	StatePath      string
	RootFolderName string
	ProcessCWD     string
	MaxFileBytes   int64
}

type PreviewDriveCleanupResult struct {
	DeletedFileCount      int   `json:"deleted_file_count"`
	DeletedEstimatedBytes int64 `json:"deleted_estimated_bytes"`
}

type DriveLocalFileLinkRewriter struct {
	store  *DriveArtifactStore
	config LocalFileLinkConfig
	mu     sync.Mutex
}

type previewDriveAPI interface {
	CreateFolder(context.Context, string, string) (previewRemoteNode, error)
	ListFiles(context.Context, string) ([]previewRemoteNode, error)
	UploadFile(context.Context, string, string, string) (string, error)
	QueryMetaURL(context.Context, string, string) (string, error)
	GrantPermission(context.Context, string, string, previewPrincipal) error
	DeleteFile(context.Context, string, string) error
}

type previewRemoteNode struct {
	Token       string
	URL         string
	Type        string
	Name        string
	CreatedTime time.Time
}

type previewPrincipal struct {
	Key        string
	MemberType string
	MemberID   string
	Type       string
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

func NewDriveLocalFileLinkRewriter(api previewDriveAPI, cfg LocalFileLinkConfig) *DriveLocalFileLinkRewriter {
	if cfg.RootFolderName == "" {
		cfg.RootFolderName = defaultPreviewRootFolderName
	}
	if cfg.MaxFileBytes < 0 {
		cfg.MaxFileBytes = defaultPreviewMaxFileBytes
	}
	return &DriveLocalFileLinkRewriter{
		store: NewDriveArtifactStore(api, ArtifactStoreConfig{
			RootFolderName: cfg.RootFolderName,
			MaxFileBytes:   cfg.MaxFileBytes,
		}),
		config: cfg,
	}
}

func (a *Adapter) RewriteLocalFileLinks(ctx context.Context, req LocalFileLinkRewriteRequest) (string, error) {
	rewriter := a.ensureLocalFileLinkRewriter()
	if rewriter == nil {
		return req.Text, nil
	}
	return rewriter.RewriteText(ctx, req)
}

func (a *Adapter) CleanupLocalFileLinksBefore(ctx context.Context, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	rewriter := a.ensureLocalFileLinkRewriter()
	if rewriter == nil {
		return PreviewDriveCleanupResult{}, nil
	}
	return rewriter.CleanupBefore(ctx, cutoff)
}

func (a *Adapter) ensureLocalFileLinkRewriter() *DriveLocalFileLinkRewriter {
	a.artifactMu.Lock()
	defer a.artifactMu.Unlock()
	if a.client == nil {
		return nil
	}
	if a.localFileLinkRewriter != nil {
		return a.localFileLinkRewriter
	}
	a.localFileLinkRewriter = NewDriveLocalFileLinkRewriter(NewLarkDrivePreviewAPI(a.client), LocalFileLinkConfig{
		ProcessCWD:     a.localFileLinkProcessCWD,
		RootFolderName: defaultPreviewRootFolderName,
	})
	return a.localFileLinkRewriter
}

func (p *DriveLocalFileLinkRewriter) RewriteText(ctx context.Context, req LocalFileLinkRewriteRequest) (string, error) {
	text := strings.TrimSpace(req.Text)
	if p == nil || p.store == nil || strings.TrimSpace(text) == "" {
		return req.Text, nil
	}
	principals := previewPrincipals(req.ChatID, req.UserID)
	if len(principals) == 0 {
		return req.Text, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	matches := localFileLinkRe.FindAllStringSubmatchIndex(req.Text, -1)
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
		matchStart := match[0]
		matchEnd := match[1]
		targetStart := match[2]
		targetEnd := match[3]
		rawTarget := req.Text[targetStart:targetEnd]
		builder.WriteString(req.Text[last:matchStart])
		original := req.Text[matchStart:matchEnd]
		replacement := original
		if cached, ok := rewrittenTargets[rawTarget]; ok {
			replacement = cached
			if replacement != original {
				changed = true
			}
		} else {
			url, ok, err := p.materializePreviewTargetLocked(ctx, rawTarget, req, principals)
			switch {
			case err != nil:
				errs = append(errs, err.Error())
			case ok && strings.TrimSpace(url) != "":
				replacement = formatPreviewLinkReplacement(rawTarget, url, req.WorkspaceCWD)
				rewrittenTargets[rawTarget] = replacement
				changed = true
			default:
				rewrittenTargets[rawTarget] = original
			}
		}
		builder.WriteString(replacement)
		last = matchEnd
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

func formatPreviewLinkReplacement(rawTarget, url, workspaceCWD string) string {
	displayPath := previewDisplayPath(rawTarget, workspaceCWD)
	clickableName := previewClickableName(displayPath)
	if displayPath == "" {
		displayPath = clickableName
	}
	if clickableName == "" {
		clickableName = "preview.md"
	}
	return "`" + sanitizeInlineCodeText(displayPath) + "` [" + sanitizeMarkdownLinkLabel(clickableName) + "](" + strings.TrimSpace(url) + ")"
}

func previewDisplayPath(rawTarget, workspaceCWD string) string {
	target := strings.TrimSpace(rawTarget)
	target = strings.Trim(target, "\"'")
	if target == "" {
		return ""
	}
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		target = strings.TrimPrefix(strings.TrimSuffix(target, ">"), "<")
	}
	if idx := strings.IndexByte(target, '#'); idx >= 0 {
		target = target[:idx]
	}
	return pathdisplay.RenderWorkspaceDisplayPath(strings.TrimSpace(target), workspaceCWD)
}

func previewClickableName(displayPath string) string {
	target := strings.TrimSpace(displayPath)
	if matched := previewLineSuffixRe.FindStringSubmatch(target); len(matched) == 3 {
		target = matched[1]
	}
	target = filepath.Clean(strings.TrimSpace(target))
	name := strings.TrimSpace(filepath.Base(target))
	switch name {
	case "", ".", string(filepath.Separator):
		return "preview.md"
	default:
		return name
	}
}

func sanitizeInlineCodeText(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "`", "'")
}

func sanitizeMarkdownLinkLabel(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, `[`, `\[`)
	value = strings.ReplaceAll(value, `]`, `\]`)
	return value
}

func (p *DriveLocalFileLinkRewriter) CleanupBefore(ctx context.Context, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	if p == nil {
		return PreviewDriveCleanupResult{}, nil
	}
	if p.store == nil {
		return PreviewDriveCleanupResult{}, fmt.Errorf("local file link drive api is not available")
	}
	return p.store.CleanupBefore(ctx, cutoff)
}

func (p *DriveLocalFileLinkRewriter) materializePreviewTargetLocked(ctx context.Context, rawTarget string, req LocalFileLinkRewriteRequest, principals []previewPrincipal) (string, bool, error) {
	resolvedPath, ok, err := p.resolvePreviewPath(rawTarget, req)
	if err != nil || !ok {
		return "", ok, err
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", true, fmt.Errorf("stat local file link source %s: %w", resolvedPath, err)
	}
	if info.Size() == 0 {
		return "", true, fmt.Errorf("skip empty local file link source %s", resolvedPath)
	}
	if p.config.MaxFileBytes > 0 && info.Size() > p.config.MaxFileBytes {
		return "", true, fmt.Errorf("local file link source exceeds %d bytes: %s", p.config.MaxFileBytes, resolvedPath)
	}
	_ = principals
	result, err := p.store.UploadLocalFile(ctx, ArtifactUploadRequest{
		LocalPath: resolvedPath,
		ChatID:    req.ChatID,
		UserID:    req.UserID,
	})
	if err != nil {
		return "", true, fmt.Errorf("upload local file link for %s: %w", resolvedPath, err)
	}
	return result.URL, true, nil
}

func (p *DriveLocalFileLinkRewriter) ensureRootFolderLocked(ctx context.Context) (*previewFolderRecord, error) {
	if p == nil || p.store == nil {
		return nil, fmt.Errorf("local file link drive api is not available")
	}
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	return p.store.ensureRootFolderLocked(ctx)
}

func (p *DriveLocalFileLinkRewriter) listRootFoldersLocked(ctx context.Context) ([]*previewFolderRecord, error) {
	if p == nil || p.store == nil {
		return nil, nil
	}
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	return p.store.listRootFoldersLocked(ctx)
}

func (p *DriveLocalFileLinkRewriter) resolvePreviewPath(rawTarget string, req LocalFileLinkRewriteRequest) (string, bool, error) {
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
	if matched := previewLineSuffixRe.FindStringSubmatch(target); len(matched) == 3 {
		target = matched[1]
	}
	target = filepath.Clean(strings.TrimSpace(target))

	roots := previewAllowedRoots(req.WorkspaceCWD, p.config.ProcessCWD)
	candidates := previewPathCandidates(target, roots)
	for _, candidate := range candidates {
		resolved, err := canonicalPreviewPath(candidate)
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
		resolved, err := canonicalPreviewPath(value)
		if err != nil {
			continue
		}
		if _, exists := seen[resolved]; exists {
			continue
		}
		seen[resolved] = struct{}{}
		roots = append(roots, resolved)
	}
	sort.Strings(roots)
	return roots
}

func canonicalPreviewPath(value string) (string, error) {
	resolved, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	resolved = filepath.Clean(resolved)
	if real, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = filepath.Clean(real)
	}
	return resolved, nil
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
			Token:       stringPtrValue(item.Token),
			URL:         stringPtrValue(item.Url),
			Type:        stringPtrValue(item.Type),
			Name:        stringPtrValue(item.Name),
			CreatedTime: parseDriveNodeCreatedTime(stringPtrValue(item.CreatedTime)),
		})
	}
	return out, nil
}

func parseDriveNodeCreatedTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if unixSeconds, err := strconv.ParseInt(raw, 10, 64); err == nil && unixSeconds > 0 {
		return time.Unix(unixSeconds, 0).UTC()
	}
	return time.Time{}
}

func (a *larkDrivePreviewAPI) UploadFile(ctx context.Context, parentToken, fileName, localPath string) (string, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return "", fmt.Errorf("stat local upload file %s: %w", localPath, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path %q is not a regular file", localPath)
	}
	if info.Size() <= 0 {
		return "", fmt.Errorf("file is empty: %s", localPath)
	}
	if info.Size() > int64(^uint(0)>>1) {
		return "", fmt.Errorf("file is too large: %s", localPath)
	}

	prepareResp, err := a.client.Drive.V1.File.UploadPrepare(ctx, larkdrive.NewUploadPrepareFileReqBuilder().
		FileUploadInfo(larkdrive.NewFileUploadInfoBuilder().
			FileName(fileName).
			ParentType("explorer").
			ParentNode(parentToken).
			Size(int(info.Size())).
			Build()).
		Build())
	if err != nil {
		return "", wrapPermissionIssue(err, permissionIssueFromDirectError("drive.file.upload_prepare", err))
	}
	if !prepareResp.Success() {
		return "", &driveAPIError{
			Code:  prepareResp.Code,
			Msg:   prepareResp.Msg,
			Issue: permissionIssueFromCodeError("drive.file.upload_prepare", prepareResp.Code, prepareResp.Msg, &prepareResp.CodeError, prepareResp.ApiResp, nil),
		}
	}
	if prepareResp.Data == nil || strings.TrimSpace(stringPtrValue(prepareResp.Data.UploadId)) == "" {
		return "", fmt.Errorf("missing upload prepare response data")
	}
	uploadID := stringPtrValue(prepareResp.Data.UploadId)
	blockSize := intPtrValue(prepareResp.Data.BlockSize)
	if blockSize <= 0 {
		blockSize = 4 * 1024 * 1024
	}

	f, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("open local upload file %s: %w", localPath, err)
	}
	defer f.Close()

	buf := make([]byte, blockSize)
	uploadedBlocks := 0
	for seq := 0; ; seq++ {
		n, readErr := io.ReadFull(f, buf)
		if readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return "", fmt.Errorf("read upload chunk %d for %s: %w", seq, localPath, readErr)
		}
		if n == 0 {
			break
		}
		partResp, err := a.client.Drive.V1.File.UploadPart(ctx, larkdrive.NewUploadPartFileReqBuilder().
			Body(larkdrive.NewUploadPartFileReqBodyBuilder().
				UploadId(uploadID).
				Seq(seq).
				Size(n).
				File(bytes.NewReader(buf[:n])).
				Build()).
			Build())
		if err != nil {
			return "", wrapPermissionIssue(err, permissionIssueFromDirectError("drive.file.upload_part", err))
		}
		if !partResp.Success() {
			return "", &driveAPIError{
				Code:  partResp.Code,
				Msg:   partResp.Msg,
				Issue: permissionIssueFromCodeError("drive.file.upload_part", partResp.Code, partResp.Msg, &partResp.CodeError, partResp.ApiResp, nil),
			}
		}
		uploadedBlocks++
		if readErr == io.ErrUnexpectedEOF {
			break
		}
	}
	finishResp, err := a.client.Drive.V1.File.UploadFinish(ctx, larkdrive.NewUploadFinishFileReqBuilder().
		Body(larkdrive.NewUploadFinishFileReqBodyBuilder().
			UploadId(uploadID).
			BlockNum(uploadedBlocks).
			Build()).
		Build())
	if err != nil {
		return "", wrapPermissionIssue(err, permissionIssueFromDirectError("drive.file.upload_finish", err))
	}
	if !finishResp.Success() {
		return "", &driveAPIError{
			Code:  finishResp.Code,
			Msg:   finishResp.Msg,
			Issue: permissionIssueFromCodeError("drive.file.upload_finish", finishResp.Code, finishResp.Msg, &finishResp.CodeError, finishResp.ApiResp, nil),
		}
	}
	if finishResp.Data == nil {
		return "", fmt.Errorf("missing upload finish response data")
	}
	return stringPtrValue(finishResp.Data.FileToken), nil
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

func intPtrValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
