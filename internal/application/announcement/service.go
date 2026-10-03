package announcement

import (
	"context"
	"feidex/internal/domain/routing"
	"log/slog"
	"strings"
	"time"
)

const CommonChatType = "group_common"

type Block struct{ BlockID, Text string }
type Gateway interface {
	List(context.Context, string) ([]Block, error)
	Create(context.Context, string, string, bool) (Block, error)
	Update(context.Context, string, string, string) error
	BotAbsent(error) bool
	RateLimited(error) bool
}
type Repository interface {
	GroupAnnouncementBlock(string, string) *routing.GroupAnnouncementBlock
	SaveGroupAnnouncementBlock(*routing.GroupAnnouncementBlock) error
}
type Service struct {
	Repository Repository
	Gateway    Gateway
	Frontend   string
	Primary    func(string) bool
}
type Status struct {
	marker        string
	content       string
	stableContent string
	stableHash    string
	botOpenID     string
	updatedAt     time.Time
}

func NewStatus(marker, content, stable, hash, bot string, updated time.Time) Status {
	return Status{marker: marker, content: content, stableContent: stable, stableHash: hash, botOpenID: bot, updatedAt: updated}
}
func (s Service) SetAbsent(chatID string, absent bool) error {
	record := s.Repository.GroupAnnouncementBlock("group", chatID)
	if record == nil {
		if !absent {
			return nil
		}
		record = &routing.GroupAnnouncementBlock{FrontendID: s.Frontend, ChatType: "group", ChatID: chatID}
	}
	if record.BotAbsent == absent {
		return nil
	}
	record.BotAbsent = absent
	return s.Repository.SaveGroupAnnouncementBlock(record)
}
func (s Service) Refresh(ctx context.Context, chatID string, status, common Status) error {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil
	}
	if status.marker == "" || status.content == "" {
		return nil
	}
	st := s.Repository
	if st == nil {
		return nil
	}
	// A chat the bot has left stays in state forever (there is no handler for
	// being removed), so skip it rather than paying for a request that can only
	// fail. handleBotGroupAdded clears the mark when the bot is added back.
	if existing := st.GroupAnnouncementBlock("group", chatID); existing != nil && existing.BotAbsent {
		return nil
	}
	var (
		blocks       []Block
		blocksErr    error
		blocksLoaded bool
	)
	loadBlocks := func() ([]Block, error) {
		if blocksLoaded {
			return blocks, blocksErr
		}
		blocksLoaded = true
		blocks, blocksErr = s.Gateway.List(ctx, chatID)
		return blocks, blocksErr
	}
	if err := s.refreshCommon(ctx, chatID, common, loadBlocks); err != nil {
		if s.Gateway.BotAbsent(err) {
			_ = s.SetAbsent(chatID, true)
			return nil
		}
		return err
	}
	record := st.GroupAnnouncementBlock("group", chatID)
	if record == nil {
		record = &routing.GroupAnnouncementBlock{
			ID:         "",
			FrontendID: s.Frontend,
			ChatID:     chatID,
			ChatType:   "group",
		}
	}
	blocks, err := loadBlocks()
	if err != nil {
		if s.Gateway.RateLimited(err) {
			slog.Warn("group announcement refresh skipped by rate limit", "chat_id", chatID, "op", "list", "error", err)
			return nil
		}
		if s.Gateway.BotAbsent(err) {
			_ = s.SetAbsent(chatID, true)
			return nil
		}
		return err
	}
	block := findAnnouncementBlock(blocks, status.marker)
	if strings.TrimSpace(block.BlockID) == "" {
		block = findAnnouncementBlockByID(blocks, record.BlockID)
	}
	blockID := strings.TrimSpace(block.BlockID)
	if blockID != "" && strings.TrimSpace(record.LastContentHash) == status.stableHash && strings.Contains(block.Text, status.stableContent) {
		return nil
	}
	if blockID == "" {
		created, err := s.Gateway.Create(ctx, chatID, status.content, false)
		if err != nil {
			if s.Gateway.RateLimited(err) {
				slog.Warn("group announcement refresh skipped by rate limit", "chat_id", chatID, "op", "create", "error", err)
				return nil
			}
			if s.Gateway.BotAbsent(err) {
				_ = s.SetAbsent(chatID, true)
				return nil
			}
			return err
		}
		blockID = strings.TrimSpace(created.BlockID)
	} else if err := s.Gateway.Update(ctx, chatID, blockID, status.content); err != nil {
		if s.Gateway.RateLimited(err) {
			slog.Warn("group announcement refresh skipped by rate limit", "chat_id", chatID, "op", "update", "error", err)
			return nil
		}
		if s.Gateway.BotAbsent(err) {
			_ = s.SetAbsent(chatID, true)
			return nil
		}
		return err
	}
	if blockID == "" {
		return nil
	}
	record.FrontendID = s.Frontend
	record.ChatID = chatID
	record.ChatType = "group"
	record.BotOpenID = status.botOpenID
	record.BlockID = blockID
	record.Marker = status.marker
	record.LastContentHash = status.stableHash
	record.LastUpdatedAt = status.updatedAt.Unix()
	return st.SaveGroupAnnouncementBlock(record)
}

func (s Service) refreshCommon(ctx context.Context, chatID string, status Status, loadBlocks func() ([]Block, error)) error {
	if !s.Primary(chatID) {
		return nil
	}
	st := s.Repository
	if status.marker == "" || status.content == "" {
		return nil
	}
	if loadBlocks == nil {
		loadBlocks = func() ([]Block, error) {
			return s.Gateway.List(ctx, chatID)
		}
	}
	blocks, err := loadBlocks()
	if err != nil {
		if s.Gateway.RateLimited(err) {
			slog.Warn("group announcement common refresh skipped by rate limit", "chat_id", chatID, "op", "list", "error", err)
			return nil
		}
		return err
	}
	record := st.GroupAnnouncementBlock(CommonChatType, chatID)
	block := findAnnouncementBlock(blocks, status.marker)
	if strings.TrimSpace(block.BlockID) == "" && record != nil {
		block = findAnnouncementBlockByID(blocks, record.BlockID)
	}
	blockID := strings.TrimSpace(block.BlockID)
	if blockID == "" {
		created, err := s.Gateway.Create(ctx, chatID, status.content, true)
		if err != nil {
			if s.Gateway.RateLimited(err) {
				slog.Warn("group announcement common refresh skipped by rate limit", "chat_id", chatID, "op", "create", "error", err)
				return nil
			}
			return err
		}
		blockID = strings.TrimSpace(created.BlockID)
	} else if !strings.Contains(block.Text, status.stableContent) {
		if err := s.Gateway.Update(ctx, chatID, blockID, status.content); err != nil {
			if s.Gateway.RateLimited(err) {
				slog.Warn("group announcement common refresh skipped by rate limit", "chat_id", chatID, "op", "update", "error", err)
				return nil
			}
			return err
		}
	}
	if blockID == "" {
		return nil
	}
	if record == nil {
		record = &routing.GroupAnnouncementBlock{
			ID:         "",
			FrontendID: s.Frontend,
			ChatID:     chatID,
			ChatType:   CommonChatType,
		}
	}
	record.FrontendID = s.Frontend
	record.ChatID = chatID
	record.ChatType = CommonChatType
	record.BotOpenID = status.botOpenID
	record.BlockID = blockID
	record.Marker = status.marker
	record.LastContentHash = status.stableHash
	record.LastUpdatedAt = status.updatedAt.Unix()
	return st.SaveGroupAnnouncementBlock(record)
}

func findAnnouncementBlockByID(blocks []Block, blockID string) Block {
	blockID = strings.TrimSpace(blockID)
	if blockID == "" {
		return Block{}
	}
	for _, block := range blocks {
		if strings.TrimSpace(block.BlockID) == blockID {
			block.BlockID = blockID
			return block
		}
	}
	return Block{}
}

func findAnnouncementBlock(blocks []Block, markers ...string) Block {
	if len(markers) == 0 {
		return Block{}
	}
	for _, block := range blocks {
		for _, marker := range markers {
			marker = strings.TrimSpace(marker)
			if marker != "" && strings.Contains(block.Text, marker) && strings.TrimSpace(block.BlockID) != "" {
				block.BlockID = strings.TrimSpace(block.BlockID)
				return block
			}
		}
	}
	return Block{}
}
