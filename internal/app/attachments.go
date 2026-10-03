package app

import (
	"context"
	"feidex/internal/adapter/feishu/attachments"
	"feidex/internal/config"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/feishu"
	"fmt"
	"strings"
	"time"
)

func resolveInboundAttachments(a *App, msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]domainsubmission.SubmissionAttachment, error) {
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

	dir := attachments.SessionAttachmentDir(workspace.Cwd, sessionKey, msg.MessageID)
	ctx, cancel := context.WithTimeout(a.Context(), 30*time.Second)
	defer cancel()

	att := make([]domainsubmission.SubmissionAttachment, 0, len(msg.Attachments))
	for _, attachment := range msg.Attachments {
		sourceMessageID := strings.TrimSpace(attachment.SourceMessageID)
		if sourceMessageID == "" {
			sourceMessageID = strings.TrimSpace(msg.MessageID)
		}
		path, name, err := a.feishu.DownloadMessageResource(ctx, sourceMessageID, attachment, dir)
		if err != nil {
			return nil, err
		}
		att = append(att, domainsubmission.SubmissionAttachment{
			Kind:      attachment.Kind,
			Name:      name,
			LocalPath: path,
		})
	}
	return att, nil
}

// Thin wrappers delegating to attachments sub-package.
