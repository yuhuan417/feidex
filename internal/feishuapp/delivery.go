package feishuapp

import (
	"context"

	appdelivery "feidex/internal/adapter/feishu/delivery"
	"log/slog"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/channel/outbound"
)

// replyTextChunked replies in chunks when the body is too long for one message.
//
// The text fallback carries the same body the card was rendered from, so it can
// be arbitrarily long; sending it whole meant a long reply plus a failed card
// send yielded a message that failed on its own, leaving the user with nothing.
// Splitting keeps code fences intact, which matters for agent output.
//
// Returns the first delivered message id. If the first chunk fails the error is
// propagated; if a later chunk fails, what already reached the user is reported
// as delivered so the caller can still record the link.
func replyTextChunked(ctx context.Context, client feishuTextReplier, messageID, text string, inThread bool) (string, error) {
	chunks := outbound.SplitWithCodeFences(text, appdelivery.ReplyTextMaxBytes)
	firstID := ""
	for i, chunk := range chunks {
		id, err := client.ReplyTextWithID(ctx, messageID, chunk, inThread)
		if err != nil {
			if firstID == "" {
				return "", err
			}
			slog.Warn("feishu reply text fallback partially delivered",
				"message_id", messageID,
				"chunk", i+1,
				"chunks", len(chunks),
				"error", err,
			)
			return firstID, nil
		}
		if firstID == "" {
			firstID = strings.TrimSpace(id)
		}
	}
	return firstID, nil
}

// feishuTextReplier is the slice of the Feishu client the chunked fallback
// needs, so the helper stays testable without a full client.
type feishuTextReplier interface {
	ReplyTextWithID(context.Context, string, string, bool) (string, error)
}

func outboundMessageCardMeta(kind string, workspaceID ...string) (title, color string, replyClass bool, showHeader bool) {
	var base string
	switch strings.TrimSpace(kind) {
	case "final_message":
		base, color, replyClass, showHeader = "最终答复", "green", true, true
	case "turn_output":
		base, color, replyClass, showHeader = "反馈中", "blue", true, true
	case "turn_reasoning":
		base, color, replyClass, showHeader = "思考", "grey", false, true
	case "turn_command_execution":
		base, color, replyClass, showHeader = "命令执行", "blue", false, true
	case "turn_file_change":
		base, color, replyClass, showHeader = "文件改动", "orange", false, true
	case "turn_plan":
		base, color, replyClass, showHeader = "计划更新", "blue", false, true
	case "turn_queued":
		base, color, replyClass, showHeader = "排队中", "grey", false, true
	case "turn_started":
		base, color, replyClass, showHeader = "开始处理", "blue", false, true
	case "turn_terminal":
		base, color, replyClass, showHeader = "任务状态", "grey", false, true
	default:
		base, color, replyClass, showHeader = "状态更新", "blue", false, true
	}
	ws := ""
	if len(workspaceID) > 0 {
		ws = strings.TrimSpace(workspaceID[0])
	}
	if ws != "" {
		title = "[" + ws + "] " + base
	} else {
		title = base
	}
	return
}
