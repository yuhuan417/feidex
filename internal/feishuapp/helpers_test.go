package feishuapp

import (
	"feidex/internal/config"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/feishu"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInboundAttachmentsUsesForwardedMessageID(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = workspace
	downloadPath := filepath.Join(t.TempDir(), "forwarded.png")
	if err := os.WriteFile(downloadPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile(downloadPath) error = %v", err)
	}
	stub := &downloadFeishuStub{
		fakeFeishuClient: &fakeFeishuClient{},
		downloadPath:     downloadPath,
	}
	a := &App{cfg: cfg, feishu: stub}

	attachments, err := resolveInboundAttachments(a, &feishu.InboundMessage{
		MessageID: "root-message",
		Attachments: []feishu.Attachment{{
			Kind:            "image",
			ResourceKey:     "img-forwarded",
			SourceMessageID: "forwarded-message",
		}},
	}, cfg.Workspaces[0].ID, "sess-1")
	if err != nil {
		t.Fatalf("resolveInboundAttachments() error = %v", err)
	}
	if len(stub.messageIDs) != 1 || stub.messageIDs[0] != "forwarded-message" {
		t.Fatalf("resolveInboundAttachments() download message ids = %+v, want forwarded source", stub.messageIDs)
	}
	if len(attachments) != 1 || attachments[0].LocalPath != downloadPath {
		t.Fatalf("resolveInboundAttachments() attachments = %+v, want downloaded file", attachments)
	}
}

func TestDeliveryHelpers(t *testing.T) {
	var a *App
	if got := sendFinalMessages(a, nil, nil, "ignored", false); got != nil {
		t.Fatalf("sendFinalMessages(nil app) = %+v, want nil", got)
	}
	a = &App{cfg: config.Default()}
	if got := sendReplyMessages(a, nil, &domainsubmission.Submission{}, "ignored", false, "final_message"); got != nil {
		t.Fatalf("sendReplyMessages(without feishu) = %+v, want nil", got)
	}
}
