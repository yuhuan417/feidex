package feishuapp

import (
	"context"
	"feidex/internal/application/announcement"
	"feidex/internal/feishu"
)

type announcementGateway struct{ client FeishuClient }

func AnnouncementGateway(a *App) announcement.Gateway { return announcementGateway{client: a.feishu} }

func (g announcementGateway) List(ctx context.Context, chatID string) ([]announcement.Block, error) {
	blocks, err := g.client.ListAnnouncementBlocks(ctx, chatID)
	out := make([]announcement.Block, len(blocks))
	for i, block := range blocks {
		out[i] = announcement.Block{BlockID: block.BlockID, Text: block.Text}
	}
	return out, err
}
func (g announcementGateway) Create(ctx context.Context, chatID, content string, top bool) (announcement.Block, error) {
	var block feishu.AnnouncementBlock
	var err error
	if top {
		block, err = g.client.CreateAnnouncementTextBlockAt(ctx, chatID, chatID, content, "", 0)
	} else {
		block, err = g.client.CreateAnnouncementTextBlock(ctx, chatID, chatID, content, "")
	}
	return announcement.Block{BlockID: block.BlockID, Text: block.Text}, err
}
func (g announcementGateway) Update(ctx context.Context, chatID, blockID, content string) error {
	return g.client.UpdateAnnouncementTextBlock(ctx, chatID, blockID, content, "")
}
func (g announcementGateway) BotAbsent(err error) bool   { return feishu.IsAnnouncementBotAbsent(err) }
func (g announcementGateway) RateLimited(err error) bool { return feishu.IsAnnouncementRateLimit(err) }
