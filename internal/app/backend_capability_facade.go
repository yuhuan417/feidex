package app

import "strings"

type backendFeature string

const (
	backendFeatureReview                      backendFeature = "review"
	backendFeatureSkills                      backendFeature = "skills"
	backendFeatureFastMode                    backendFeature = "fastMode"
	backendFeatureConversationThreadCommands  backendFeature = "conversationThreadCommands"
	backendFeatureConversationSessionCommands backendFeature = "conversationSessionCommands"
	backendFeatureConversationPermissions     backendFeature = "conversationPermissions"
	backendFeatureWorkspacePermissions        backendFeature = "workspacePermissions"
)

type conversationPresentation struct {
	Slash          string
	Noun           string
	MenuLabel      string
	IDLabel        string
	IDPlaceholder  string
	SummaryLabel   string
	HelpGroupLabel string
}

type backendCapabilitySpec struct {
	kind         string
	conversation conversationPresentation
	features     map[backendFeature]bool
}

func backendCapabilityKinds() []string {
	return []string{backendCodex, backendClaude}
}

func backendCapabilityForKind(kind string) backendCapabilitySpec {
	switch normalizeRuntimeBackend(kind) {
	case backendClaude:
		return backendCapabilitySpec{
			kind: backendClaude,
			conversation: conversationPresentation{
				Slash:          "/session",
				Noun:           "会话",
				MenuLabel:      "会话管理",
				IDLabel:        "session id",
				IDPlaceholder:  "SESSION_ID",
				SummaryLabel:   "session",
				HelpGroupLabel: "session",
			},
			features: map[backendFeature]bool{
				backendFeatureConversationSessionCommands: true,
				backendFeatureConversationPermissions:     true,
				backendFeatureWorkspacePermissions:        true,
			},
		}
	default:
		return backendCapabilitySpec{
			kind: backendCodex,
			conversation: conversationPresentation{
				Slash:          "/thread",
				Noun:           "线程",
				MenuLabel:      "线程管理",
				IDLabel:        "thread id",
				IDPlaceholder:  "THREAD_ID",
				SummaryLabel:   "thread",
				HelpGroupLabel: "thread",
			},
			features: map[backendFeature]bool{
				backendFeatureReview:                     true,
				backendFeatureSkills:                     true,
				backendFeatureFastMode:                   true,
				backendFeatureConversationThreadCommands: true,
			},
		}
	}
}

func (s backendCapabilitySpec) supports(feature backendFeature) bool {
	return s.features[feature]
}

func (s backendCapabilitySpec) currentConversationLabel() string {
	return "当前" + s.conversation.Noun
}

func (s backendCapabilitySpec) missingConversationLabel() string {
	return "当前没有活动" + s.conversation.Noun
}

func (s backendCapabilitySpec) rewriteConversationText(text string) string {
	if s.kind != backendClaude {
		return text
	}
	text = strings.ReplaceAll(text, "线程", s.conversation.Noun)
	text = strings.ReplaceAll(text, "thread", s.conversation.SummaryLabel)
	return text
}

func (s backendCapabilitySpec) rewriteHelpEntries(specs []helpCommandSpec) []helpCommandSpec {
	updated := make([]helpCommandSpec, 0, len(specs))
	for _, spec := range specs {
		item := spec
		item.Command = strings.ReplaceAll(item.Command, "/thread", s.conversation.Slash)
		item.Command = strings.ReplaceAll(item.Command, "THREAD_ID", s.conversation.IDPlaceholder)
		item.Summary = s.rewriteConversationText(item.Summary)
		updated = append(updated, item)
	}
	return updated
}

func (s backendCapabilitySpec) helpGroupLabel(group string) string {
	group = strings.TrimSpace(group)
	if group == "thread" {
		return s.conversation.HelpGroupLabel
	}
	return group
}

func (s backendCapabilitySpec) menuGroupSpec(action string, spec commandMenuGroupSpec) commandMenuGroupSpec {
	if strings.TrimSpace(action) == "menu.thread" {
		spec.Label = s.conversation.MenuLabel
		spec.Description = "查看当前" + s.conversation.Noun + "状态，并通过下拉切换" + s.conversation.Noun + "。"
	}
	return spec
}

func (s backendCapabilitySpec) menuNodeLabel(action, label string) string {
	switch strings.TrimSpace(action) {
	case "menu.thread":
		return s.conversation.MenuLabel
	case "thread.permission_mode.menu":
		if s.supports(backendFeatureConversationPermissions) {
			return "会话权限"
		}
	case "workspace.permission_mode.menu":
		if s.supports(backendFeatureWorkspacePermissions) {
			return "默认权限"
		}
	}
	return strings.TrimSpace(label)
}

func backendConversationHelpEntries(backend string, specs []helpCommandSpec) []helpCommandSpec {
	return backendCapabilityForKind(backend).rewriteHelpEntries(specs)
}

func backendCommandPolicy(backend string, policy localCommandBackendSpec) map[string]localCommandBackendSpec {
	backend = normalizeRuntimeBackend(backend)
	if backend == "" {
		return nil
	}
	return map[string]localCommandBackendSpec{backend: policy}
}

func backendPoliciesForUnsupportedFeature(feature backendFeature) map[string]localCommandBackendSpec {
	policies := map[string]localCommandBackendSpec{}
	for _, backend := range backendCapabilityKinds() {
		if backendCapabilityForKind(backend).supports(feature) {
			continue
		}
		policies[backend] = hiddenBackendCommand()
	}
	if len(policies) == 0 {
		return nil
	}
	return policies
}
