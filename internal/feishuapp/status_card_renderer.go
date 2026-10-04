package feishuapp

import "feidex/internal/feishu"

type simpleStatusCardClient interface {
	SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any
}

type simpleStatusCardRenderer struct{ client simpleStatusCardClient }

func (r simpleStatusCardRenderer) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	if r.client == nil {
		return nil
	}
	return r.client.SimpleStatusCard(title, color, body, buttons)
}
