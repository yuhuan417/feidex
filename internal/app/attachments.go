package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/app/attachments"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const attachmentsDirName = attachments.AttachmentsDirName

func resolveInboundAttachments(a *App, msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]state.SubmissionAttachment, error) {
	if msg == nil || len(msg.Attachments) == 0 {
		return nil, nil
	}
	workspace := config.FindWorkspace(a.cfg, workspaceID)
	if workspace == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	if strings.TrimSpace(msg.MessageID) == "" {
		return nil, fmt.Errorf("attachment message is missing message id")
	}

	dir := sessionAttachmentDir(workspace.Cwd, sessionKey, msg.MessageID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	att := make([]state.SubmissionAttachment, 0, len(msg.Attachments))
	for _, attachment := range msg.Attachments {
		sourceMessageID := strings.TrimSpace(attachment.SourceMessageID)
		if sourceMessageID == "" {
			sourceMessageID = strings.TrimSpace(msg.MessageID)
		}
		path, name, err := a.feishu.DownloadMessageResource(ctx, sourceMessageID, attachment, dir)
		if err != nil {
			return nil, err
		}
		att = append(att, state.SubmissionAttachment{
			Kind:      attachment.Kind,
			Name:      name,
			LocalPath: path,
		})
	}
	return att, nil
}

// Thin wrappers delegating to attachments sub-package.

var sessionAttachmentDir = attachments.SessionAttachmentDir

var shortHash = attachments.ShortHash

var buildTurnInputs = attachments.BuildTurnInputs

var textInput = attachments.TextInput

var attachmentPrompt = attachments.AttachmentPrompt

var submissionInputPreview = attachments.SubmissionInputPreview

var attachmentPreview = attachments.AttachmentPreview

var normalizeReferencedPath = attachments.NormalizeReferencedPath

var repairMalformedWorkspacePath = attachments.RepairMalformedWorkspacePath

var sanitizeLocalMarkdownLinks = attachments.SanitizeLocalMarkdownLinks

var neutralizeLocalMarkdownLinks = attachments.NeutralizeLocalMarkdownLinks

var rewriteMarkdownLinksForCard = attachments.RewriteMarkdownLinksForCard

var localLinkDisplayTarget = attachments.LocalLinkDisplayTarget

var cleanMarkdownLinkTarget = attachments.CleanMarkdownLinkTarget

var looksLikeLocalPathTarget = attachments.LooksLikeLocalPathTarget

var recoverFilenameFromMalformedLabel = attachments.RecoverFilenameFromMalformedLabel

var isAlphaNum = attachments.IsAlphaNum

var isFileNameLike = attachments.IsFileNameLike

var trimLineReferenceSuffix = attachments.TrimLineReferenceSuffix

var pathWithinWorkspace = attachments.PathWithinWorkspace
