package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/attachments"
	"feidex/internal/adapter/feishu/finalcardpatch"
	domainsubmission "feidex/internal/domain/submission"
	frontendruntime "feidex/internal/runtime"
	"log/slog"
	"strings"
	"time"

	applinkutil "feidex/internal/adapter/feishu/linkutil"
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

type localFileLinkRewriter interface {
	RewriteLocalFileLinks(context.Context, feishu.LocalFileLinkRewriteRequest) (string, error)
}

func rewriteLocalFileLinksText(cfg *config.Config, client localFileLinkRewriter, ctx context.Context, sub *domainsubmission.Submission, text string) string {
	text = strings.TrimSpace(text)
	if client == nil || sub == nil || text == "" {
		return text
	}
	ws := config.FindWorkspace(cfg, sub.WorkspaceID)
	if ws == nil {
		return text
	}
	normalized := applinkutil.NormalizeLocalFilePreviewTargets(text, ws.Cwd)
	rewritten, err := client.RewriteLocalFileLinks(ctx, feishu.LocalFileLinkRewriteRequest{
		Text:         normalized,
		WorkspaceCWD: ws.Cwd,
		ChatID:       sub.ChatID,
		UserID:       sub.UserID,
	})
	if err != nil {
		slog.Warn("local file link rewrite failed", "submission_id", sub.ID, "workspace_id", sub.WorkspaceID, "error", err)
	}
	if strings.TrimSpace(rewritten) == "" {
		return text
	}
	rewritten = strings.TrimSpace(rewritten)
	if rewritten == strings.TrimSpace(normalized) && normalized != text {
		return text
	}
	return rewritten
}

type localFileLinkPatcher struct {
	cfg         *config.Config
	state       planmode.SessionStateProvider
	client      localFileLinkRewriter
	lifecycle   *frontendruntime.FrontendRuntime
	asyncRunner func(func())
	finalCards  finalcardpatch.Service
	outbound    effectOutbound
	ready       bool
}

func newLocalFileLinkPatcher(cfg *config.Config, state planmode.SessionStateProvider, client localFileLinkRewriter, lifecycle *frontendruntime.FrontendRuntime, asyncRunner func(func()), finalCards finalcardpatch.Service, outbound effectOutbound, ready bool) localFileLinkPatcher {
	return localFileLinkPatcher{cfg: cfg, state: state, client: client, lifecycle: lifecycle, asyncRunner: asyncRunner, finalCards: finalCards, outbound: outbound, ready: ready}
}

func (p localFileLinkPatcher) Schedule(sub *domainsubmission.Submission, messageID, title, color string, showHeader bool, body string, footerLines []string) {
	messageID = strings.TrimSpace(messageID)
	body = strings.TrimSpace(body)
	if !p.ready || p.client == nil || p.lifecycle == nil || sub == nil || messageID == "" || body == "" {
		return
	}
	managed := p.finalCards.MarkFinalCardPreviewPending(messageID)
	if !p.lifecycle.Run(func() {
		if managed {
			defer p.finalCards.MarkFinalCardPreviewDone(messageID)
		}
		ctx, cancel := context.WithTimeout(p.lifecycle.Context(), 2*time.Minute)
		defer cancel()
		rewritten := rewriteLocalFileLinksText(p.cfg, p.client, ctx, sub, body)
		if strings.TrimSpace(rewritten) == "" || strings.TrimSpace(rewritten) == body {
			return
		}
		if managed && p.finalCards.UpdateFinalCardPatchBody(messageID, rewritten) {
			return
		}
		card := newCardRenderer(p.cfg).renderReplyMarkdownCardWithHeaderOptions(p.lifecycle.Context(), sub, contentCardTitleForSubmission(p.state, sub, title), color, showHeader, rewritten, nil, true)
		appendReplyCardFooter(card, footerLines)
		patchCtx, patchCancel := context.WithTimeout(p.lifecycle.Context(), 15*time.Second)
		defer patchCancel()
		if err := p.outbound.PatchCard(patchCtx, messageID, card); err != nil {
			slog.Warn("local file link patch failed", "submission_id", sub.ID, "workspace_id", sub.WorkspaceID, "message_id", messageID, "error", err)
		}
	}, p.asyncRunner) && managed {
		p.finalCards.MarkFinalCardPreviewDone(messageID)
	}
}

func prepareReplyCardMarkdown(cfg *config.Config, ctx context.Context, sub *domainsubmission.Submission, text string, enablePreview bool) string {
	text = strings.TrimSpace(text)
	if enablePreview {
		text = applinkutil.LinkifyInlineCodeURLs(text)
		if sub != nil {
			if ws := config.FindWorkspace(cfg, sub.WorkspaceID); ws != nil {
				text = attachments.NeutralizeLocalMarkdownLinks(text, ws.Cwd)
			}
		}
		return applinkutil.NormalizeCardMarkdown(text)
	}
	return newCardRenderer(cfg).prepareCardMarkdown(sub, text)
}

func scheduleLocalFileLinkPatch(a *App, sub *domainsubmission.Submission, messageID, title, color string, showHeader bool, body string, footerLines []string) {
	if a == nil {
		return
	}
	newLocalFileLinkPatcher(a.Config(), a.State(), a.feishu, &a.runtimeOwner.Lifecycle, a.asyncRunner, a.bindings.FinalCardPatch, newEffectOutbound(a.FrontendID(), newEffectRunner(a.runtimeOwner)), a.feishu != nil).Schedule(sub, messageID, title, color, showHeader, body, footerLines)
}
