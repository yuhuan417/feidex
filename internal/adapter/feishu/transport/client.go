package transport

import (
	"context"
	"time"

	"feidex/internal/feishu"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// Client describes Feishu transport capabilities. Business outbound calls use
// EffectClient; the effect runner binds directly to the notifying transport.
type Client interface {
	SetHandlers(func(*feishu.InboundMessage), func(*feishu.CardAction) (*callback.CardActionTriggerResponse, error), func(*feishu.MessageRecall), func(*feishu.MessageReaction))
	Start(context.Context) error
	Stop()
	ConfigureLocalFileLinks(string, string)
	RewriteLocalFileLinks(context.Context, feishu.LocalFileLinkRewriteRequest) (string, error)
	CleanupArtifactsBefore(context.Context, time.Time) (feishu.PreviewDriveCleanupResult, error)
	AddReaction(context.Context, string, string) error
	RemoveReaction(context.Context, string, string) error
	ReplyText(context.Context, string, string, bool) error
	ReplyTextWithID(context.Context, string, string, bool) (string, error)
	SendText(context.Context, string, string) error
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	SendCard(context.Context, string, map[string]any) (string, error)
	PatchCard(context.Context, string, map[string]any) error
	ReplyLocalAttachment(context.Context, string, string, bool) error
	ReplyLocalImage(context.Context, string, string, bool) error
	ReplyLocalVideo(context.Context, string, string, bool) error
	DownloadMessageResource(context.Context, string, feishu.Attachment, string) (string, string, error)
	ResolveMergeForward(context.Context, string, []string) (string, []feishu.Attachment, error)
	ShareLocalFile(context.Context, feishu.SharedFileRequest) (feishu.SharedFileResult, error)
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
	UrgentApp(context.Context, string, string) error
	LookupMessageSenderOpenID(context.Context, string) (string, error)
	GetGroupBotCount(context.Context, string) (int, error)
	ListAnnouncementBlocks(context.Context, string) ([]feishu.AnnouncementBlock, error)
	CreateAnnouncementTextBlock(context.Context, string, string, string, string) (feishu.AnnouncementBlock, error)
	CreateAnnouncementTextBlockAt(context.Context, string, string, string, string, int) (feishu.AnnouncementBlock, error)
	UpdateAnnouncementTextBlock(context.Context, string, string, string, string) error
	BotOpenID() string
	BotName() string
}
