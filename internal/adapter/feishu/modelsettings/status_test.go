package modelsettings

import (
	"feidex/internal/application/modelconfig"
	"strings"
	"testing"
)

func TestStatusRendersConfirmedApplicationSeparatelyFromDesired(t *testing.T) {
	body := RenderStatus(modelconfig.StatusView{Backend: "codex", HasSession: true, Status: modelconfig.Status{
		NextModel: "desired", NextEffort: "high", HasApplied: true, AppliedModel: "confirmed", AppliedEffort: "medium", Pending: true, Error: "retry required"}})
	for _, expected := range []string{"下一轮本地启动模型：`desired`", "最近已应用模型：`confirmed`", "待对应边界生效", "retry required", "后台 goal 自动续跑不保证采用新配置"} {
		if !strings.Contains(body, expected) {
			t.Errorf("status missing %q: %s", expected, body)
		}
	}
	if strings.Contains(body, "最近已应用模型：`desired`") {
		t.Fatal("desired settings rendered as confirmed")
	}
}

func TestStatusDoesNotInventApplicationBeforeSessionExists(t *testing.T) {
	body := RenderStatus(modelconfig.StatusView{Backend: "claude"})
	if strings.Contains(body, "下一轮本地启动模型") || strings.Contains(body, "最近已应用模型") {
		t.Fatalf("absent session rendered applied values: %s", body)
	}
	body = RenderStatus(modelconfig.StatusView{Backend: "claude", HasSession: true})
	if !strings.Contains(body, "尚无已确认") || strings.Contains(body, "最近已应用模型") {
		t.Fatalf("unconfirmed session rendered applied values: %s", body)
	}
}
