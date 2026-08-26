package feishu

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

const (
	announcementLatestRevisionID = -1
	announcementTextBlockType    = 2
	announcementRateLimitCode    = 99991400
)

// AnnouncementBlock is the minimal block projection the app layer needs for
// per-bot group announcement status regions.
type AnnouncementBlock struct {
	BlockID string
	Text    string
}

// AnnouncementAPIError wraps a Feishu announcement API response failure.
type AnnouncementAPIError struct {
	Op         string
	HTTPStatus int
	Code       int
	Msg        string
}

func (e *AnnouncementAPIError) PermissionIssue() *PermissionIssue {
	if e == nil {
		return nil
	}
	return permissionIssueFromCodeError(e.Op, e.Code, e.Msg, nil, nil, e)
}

func (e *AnnouncementAPIError) Error() string {
	if e == nil {
		return "feishu announcement api error"
	}
	parts := []string{"feishu announcement api error"}
	if e.Op != "" {
		parts = append(parts, "op="+e.Op)
	}
	if e.HTTPStatus != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.HTTPStatus))
	}
	if e.Code != 0 {
		parts = append(parts, fmt.Sprintf("code=%d", e.Code))
	}
	if strings.TrimSpace(e.Msg) != "" {
		parts = append(parts, "msg="+strings.TrimSpace(e.Msg))
	}
	return strings.Join(parts, " ")
}

// IsAnnouncementRateLimit reports whether err is a Feishu announcement rate
// limit response. Callers should not retry these errors.
func IsAnnouncementRateLimit(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *AnnouncementAPIError
	if errors.As(err, &apiErr) {
		return apiErr.HTTPStatus == http.StatusTooManyRequests || apiErr.Code == announcementRateLimitCode
	}
	var codeErrPtr *larkcore.CodeError
	if errors.As(err, &codeErrPtr) && codeErrPtr != nil {
		return codeErrPtr.Code == announcementRateLimitCode
	}
	var codeErr larkcore.CodeError
	return errors.As(err, &codeErr) && codeErr.Code == announcementRateLimitCode
}

// ListAnnouncementBlocks lists the current upgraded group announcement blocks.
func (a *Adapter) ListAnnouncementBlocks(ctx context.Context, chatID string) ([]AnnouncementBlock, error) {
	chatID = strings.TrimSpace(chatID)
	if a == nil || chatID == "" {
		return nil, nil
	}
	var out []AnnouncementBlock
	pageToken := ""
	for {
		builder := larkdocx.NewListChatAnnouncementBlockReqBuilder().
			ChatId(chatID).
			RevisionId(announcementLatestRevisionID).
			PageSize(500)
		if pageToken != "" {
			builder.PageToken(pageToken)
		}
		resp, err := withFeishuTenantTokenRefreshRetry(ctx, a, "docx.chat_announcement_block.list", func(client *lark.Client) (*larkdocx.ListChatAnnouncementBlockResp, error) {
			return client.Docx.V1.ChatAnnouncementBlock.List(ctx, builder.Build())
		})
		if err != nil {
			a.noteOutboundTransportFailure(err)
			return nil, err
		}
		if resp == nil || !resp.Success() {
			return nil, announcementError("docx.chat_announcement_block.list", respStatus(resp), respCode(resp), respMsg(resp))
		}
		if resp.Data != nil {
			for _, block := range resp.Data.Items {
				if projected := announcementBlockFromSDK(block); projected.BlockID != "" || projected.Text != "" {
					out = append(out, projected)
				}
			}
			if resp.Data.HasMore != nil && *resp.Data.HasMore {
				pageToken = stringPtrValue(resp.Data.PageToken)
				if pageToken != "" {
					continue
				}
			}
		}
		return out, nil
	}
}

// CreateAnnouncementTextBlock appends a text block under parentBlockID. For a
// group announcement root, Feishu accepts chatID as the parent block id.
func (a *Adapter) CreateAnnouncementTextBlock(ctx context.Context, chatID, parentBlockID, content, clientToken string) (AnnouncementBlock, error) {
	chatID = strings.TrimSpace(chatID)
	parentBlockID = strings.TrimSpace(parentBlockID)
	if parentBlockID == "" {
		parentBlockID = chatID
	}
	if a == nil || chatID == "" || parentBlockID == "" {
		return AnnouncementBlock{}, nil
	}
	if delay, err := a.ensureAnnouncementPacer().Wait(ctx); err != nil {
		return AnnouncementBlock{}, err
	} else if delay > 0 {
		slog.Debug("feishu outbound paced", "op", "announcement.create", "chat_id", chatID, "delay_ms", delay.Milliseconds())
	}
	req := larkdocx.NewCreateChatAnnouncementBlockChildrenReqBuilder().
		ChatId(chatID).
		BlockId(parentBlockID).
		RevisionId(announcementLatestRevisionID).
		Body(larkdocx.NewCreateChatAnnouncementBlockChildrenReqBodyBuilder().
			Children([]*larkdocx.Block{announcementTextBlock(content)}).
			Build())
	if strings.TrimSpace(clientToken) != "" {
		req.ClientToken(strings.TrimSpace(clientToken))
	}
	resp, err := withFeishuTenantTokenRefreshRetry(ctx, a, "docx.chat_announcement_block.children.create", func(client *lark.Client) (*larkdocx.CreateChatAnnouncementBlockChildrenResp, error) {
		return client.Docx.V1.ChatAnnouncementBlockChildren.Create(ctx, req.Build())
	})
	if err != nil {
		a.noteOutboundTransportFailure(err)
		return AnnouncementBlock{}, err
	}
	if resp == nil || !resp.Success() {
		return AnnouncementBlock{}, announcementError("docx.chat_announcement_block.children.create", respStatus(resp), respCode(resp), respMsg(resp))
	}
	if resp.Data == nil || len(resp.Data.Children) == 0 {
		return AnnouncementBlock{}, fmt.Errorf("missing created announcement child block")
	}
	return announcementBlockFromSDK(resp.Data.Children[0]), nil
}

// UpdateAnnouncementTextBlock replaces the text elements for one existing text block.
func (a *Adapter) UpdateAnnouncementTextBlock(ctx context.Context, chatID, blockID, content, clientToken string) error {
	chatID = strings.TrimSpace(chatID)
	blockID = strings.TrimSpace(blockID)
	if a == nil || chatID == "" || blockID == "" {
		return nil
	}
	if delay, err := a.ensureAnnouncementPacer().Wait(ctx); err != nil {
		return err
	} else if delay > 0 {
		slog.Debug("feishu outbound paced", "op", "announcement.update", "chat_id", chatID, "block_id", blockID, "delay_ms", delay.Milliseconds())
	}
	req := larkdocx.NewBatchUpdateChatAnnouncementBlockReqBuilder().
		ChatId(chatID).
		RevisionId(announcementLatestRevisionID).
		Body(larkdocx.NewBatchUpdateChatAnnouncementBlockReqBodyBuilder().
			Requests([]*larkdocx.UpdateBlockRequest{
				larkdocx.NewUpdateBlockRequestBuilder().
					BlockId(blockID).
					UpdateTextElements(larkdocx.NewUpdateTextElementsRequestBuilder().Elements(announcementTextElements(content)).Build()).
					Build(),
			}).
			Build())
	if strings.TrimSpace(clientToken) != "" {
		req.ClientToken(strings.TrimSpace(clientToken))
	}
	resp, err := withFeishuTenantTokenRefreshRetry(ctx, a, "docx.chat_announcement_block.batch_update", func(client *lark.Client) (*larkdocx.BatchUpdateChatAnnouncementBlockResp, error) {
		return client.Docx.V1.ChatAnnouncementBlock.BatchUpdate(ctx, req.Build())
	})
	if err != nil {
		a.noteOutboundTransportFailure(err)
		return err
	}
	if resp == nil || !resp.Success() {
		return announcementError("docx.chat_announcement_block.batch_update", respStatus(resp), respCode(resp), respMsg(resp))
	}
	return nil
}

func announcementTextBlock(content string) *larkdocx.Block {
	return larkdocx.NewBlockBuilder().
		BlockType(announcementTextBlockType).
		Text(larkdocx.NewTextBuilder().Elements(announcementTextElements(content)).Build()).
		Build()
}

func announcementTextElements(content string) []*larkdocx.TextElement {
	return []*larkdocx.TextElement{
		larkdocx.NewTextElementBuilder().
			TextRun(larkdocx.NewTextRunBuilder().Content(content).Build()).
			Build(),
	}
}

func announcementBlockFromSDK(block *larkdocx.Block) AnnouncementBlock {
	if block == nil {
		return AnnouncementBlock{}
	}
	return AnnouncementBlock{BlockID: stringPtrValue(block.BlockId), Text: announcementBlockText(block)}
}

func announcementBlockText(block *larkdocx.Block) string {
	if block == nil {
		return ""
	}
	texts := []*larkdocx.Text{block.Text, block.Heading1, block.Heading2, block.Heading3, block.Heading4, block.Heading5, block.Heading6, block.Heading7, block.Heading8, block.Heading9, block.Bullet, block.Ordered, block.Code, block.Quote}
	var b strings.Builder
	for _, text := range texts {
		if text == nil {
			continue
		}
		for _, element := range text.Elements {
			if element == nil || element.TextRun == nil || element.TextRun.Content == nil {
				continue
			}
			b.WriteString(*element.TextRun.Content)
		}
	}
	return b.String()
}

func announcementError(op string, status, code int, msg string) error {
	return &AnnouncementAPIError{Op: strings.TrimSpace(op), HTTPStatus: status, Code: code, Msg: strings.TrimSpace(msg)}
}

func respStatus(resp any) int {
	switch v := resp.(type) {
	case *larkdocx.ListChatAnnouncementBlockResp:
		if v != nil && v.ApiResp != nil {
			return v.ApiResp.StatusCode
		}
	case *larkdocx.CreateChatAnnouncementBlockChildrenResp:
		if v != nil && v.ApiResp != nil {
			return v.ApiResp.StatusCode
		}
	case *larkdocx.BatchUpdateChatAnnouncementBlockResp:
		if v != nil && v.ApiResp != nil {
			return v.ApiResp.StatusCode
		}
	}
	return 0
}

func respCode(resp any) int {
	code, _, ok := feishuResponseCodeAndMessage(resp)
	if !ok {
		return 0
	}
	return code
}

func respMsg(resp any) string {
	_, msg, ok := feishuResponseCodeAndMessage(resp)
	if !ok {
		return ""
	}
	return msg
}
