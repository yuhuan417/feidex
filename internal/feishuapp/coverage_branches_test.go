package feishuapp

import (
	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"

	"context"
	"strings"
	"testing"

	"feidex/internal/feishu"
)

func TestMenuWrapperCardsAndActionUserID(t *testing.T) {
	a, _, _ := newTestApp(t)

	sessionCard := renderToolsMenuCard(a, "sess-1")
	if body := cardMarkdownContent(t, sessionCard); !strings.Contains(body, "当前位置：主菜单 / 常用工具") {
		t.Fatalf("renderToolsMenuCard() body = %q", body)
	}

	contextCard := renderCommandMenuCard(a, "sess-1")
	if body := cardMarkdownContent(t, contextCard); !strings.Contains(body, "当前位置：主菜单") {
		t.Fatalf("renderCommandMenuCard() body = %q", body)
	}

	if got := appdebugviewcmd.ActionUserID(nil); got != "" {
		t.Fatalf("appdebugviewcmd.ActionUserID(nil) = %q, want empty", got)
	}
	if got := appdebugviewcmd.ActionUserID(&feishu.CardAction{UserID: " user-1 "}); got != "user-1" {
		t.Fatalf("appdebugviewcmd.ActionUserID(trim) = %q, want user-1", got)
	}
}

func TestRenderDownloadFailedCard(t *testing.T) {
	a, _, _ := newTestApp(t)

	card := appdebugviewcmd.RenderDownloadFailedCard(DebugViewDependencies(a), "/workspace/repo/docs/report.txt", "/workspace/repo", " permission denied ")
	body := cardMarkdownContent(t, card)
	for _, want := range []string{
		"生成下载链接失败。",
		"文件: `report.txt`",
		"路径: `docs/report.txt`",
		"错误: permission denied",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("renderDownloadFailedCard() missing %q: %q", want, body)
		}
	}
}

func TestNotifyingFeishuClientStartAndStop(t *testing.T) {
	base := &fakeFeishuClient{}
	client, _ := appfeishuwrap.WrapFeishuClient(base).(*appfeishuwrap.NotifyingFeishuClient)
	if client == nil {
		t.Fatal("appfeishuwrap.WrapFeishuClient() should return notifying client")
	}
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	client.Stop()
	if !base.started || !base.stopped {
		t.Fatalf("base lifecycle = started:%v stopped:%v, want both true", base.started, base.stopped)
	}
}
