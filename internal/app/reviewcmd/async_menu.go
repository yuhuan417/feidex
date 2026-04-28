package reviewcmd

import (
	"strings"

	apputil "feidex/internal/app/apputil"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func reviewAsyncButtons(sessionKey, retryAction string) []feishu.Button {
	buttons := []feishu.Button{}
	if strings.TrimSpace(retryAction) != "" {
		buttons = append(buttons, feishu.Button{
			Text: "重试",
			Type: "primary",
			Value: map[string]any{
				"action":      retryAction,
				"session_key": sessionKey,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: "返回代码审查",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.review",
			"session_key": sessionKey,
		},
	})
	return buttons
}

func renderReviewPreparingCard(a App, sessionKey, body string) map[string]any {
	return a.ReviewFeishu().SimpleStatusCard("代码审查", "blue", a.ReviewMenuCardBody("menu.review", strings.TrimSpace(body)), nil)
}

func renderReviewFailureCard(a App, sessionKey, errText, retryAction string) map[string]any {
	body := "这次 review 操作失败了。"
	if text := strings.TrimSpace(errText); text != "" {
		body += "\n\n错误: " + text
	}
	return a.ReviewFeishu().SimpleStatusCard("代码审查", "orange", a.ReviewMenuCardBody("menu.review", body), reviewAsyncButtons(sessionKey, retryAction))
}

func renderReviewResultCard(a App, sessionKey, text string) map[string]any {
	return a.ReviewFeishu().SimpleStatusCard("代码审查", "green", a.ReviewMenuCardBody("menu.review", apputil.FirstNonEmpty(strings.TrimSpace(text), "已启动 review。")), reviewAsyncButtons(sessionKey, ""))
}

func CompleteMenuReviewUncommitted(a App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.ReviewCompleteAsyncCommandAction(
		action,
		sessionKey,
		"/review",
		"menu.review",
		"正在准备 review",
		renderReviewPreparingCard(a, sessionKey, "正在准备 review，请稍候。\n\n这张卡片会自动刷新。"),
		func(sessionKey, text string) map[string]any { return renderReviewResultCard(a, sessionKey, text) },
		func(sessionKey, errText string) map[string]any {
			return renderReviewFailureCard(a, sessionKey, errText, "menu.review.uncommitted")
		},
		"review uncommitted patch failed",
	)
}

func CompleteMenuReviewBase(a App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.ReviewCompleteAsyncCommandAction(
		action,
		sessionKey,
		"/review base",
		"menu.review",
		"正在加载 review 表单",
		renderReviewPreparingCard(a, sessionKey, "正在加载 base branch 选择，请稍候。\n\n这张卡片会自动刷新。"),
		nil,
		func(sessionKey, errText string) map[string]any {
			return renderReviewFailureCard(a, sessionKey, errText, "menu.review.base")
		},
		"review base patch failed",
	)
}

func CompleteMenuReviewCommit(a App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.ReviewCompleteAsyncCommandAction(
		action,
		sessionKey,
		"/review commit",
		"menu.review",
		"正在加载 review 表单",
		renderReviewPreparingCard(a, sessionKey, "正在加载 commit 选择，请稍候。\n\n这张卡片会自动刷新。"),
		nil,
		func(sessionKey, errText string) map[string]any {
			return renderReviewFailureCard(a, sessionKey, errText, "menu.review.commit")
		},
		"review commit patch failed",
	)
}
