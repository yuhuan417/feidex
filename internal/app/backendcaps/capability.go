package backendcaps

import (
	"strings"

	"feidex/internal/app/menutypes"
	"feidex/internal/app/runtime"
)

// Feature identifies a backend capability feature.
type Feature string

const (
	FeatureReview                      Feature = "review"
	FeatureSkills                      Feature = "skills"
	FeatureFastMode                    Feature = "fastMode"
	FeatureConversationThreadCommands  Feature = "conversationThreadCommands"
	FeatureConversationSessionCommands Feature = "conversationSessionCommands"
	FeatureConversationPermissions     Feature = "conversationPermissions"
	FeatureWorkspacePermissions        Feature = "workspacePermissions"
)

// ConversationPresentation describes how a backend labels conversations in UI.
type ConversationPresentation struct {
	Slash          string
	Noun           string
	MenuLabel      string
	IDLabel        string
	IDPlaceholder  string
	SummaryLabel   string
	HelpGroupLabel string
}

// CapabilitySpec describes the capabilities of a specific backend.
type CapabilitySpec struct {
	Kind         string
	Conversation ConversationPresentation
	Features     map[Feature]bool
}

// Kinds returns all supported backend kind strings.
func Kinds() []string {
	return []string{runtime.BackendCodex, runtime.BackendClaude}
}

// ForKind returns the capability spec for the given backend kind.
func ForKind(kind string) CapabilitySpec {
	switch runtime.NormalizeBackend(kind) {
	case runtime.BackendCodex:
		return CapabilitySpec{
			Kind: runtime.BackendCodex,
			Conversation: ConversationPresentation{
				Slash:          "/thread",
				Noun:           "线程",
				MenuLabel:      "线程管理",
				IDLabel:        "thread id",
				IDPlaceholder:  "THREAD_ID",
				SummaryLabel:   "thread",
				HelpGroupLabel: "thread",
			},
			Features: map[Feature]bool{
				FeatureReview:                     true,
				FeatureSkills:                     true,
				FeatureFastMode:                   true,
				FeatureConversationThreadCommands: true,
			},
		}
	case runtime.BackendClaude:
		return CapabilitySpec{
			Kind: runtime.BackendClaude,
			Conversation: ConversationPresentation{
				Slash:          "/session",
				Noun:           "会话",
				MenuLabel:      "会话管理",
				IDLabel:        "session id",
				IDPlaceholder:  "SESSION_ID",
				SummaryLabel:   "session",
				HelpGroupLabel: "session",
			},
			Features: map[Feature]bool{
				FeatureConversationSessionCommands: true,
				FeatureConversationPermissions:     true,
				FeatureWorkspacePermissions:        true,
			},
		}
	default:
		return CapabilitySpec{
			Kind: "",
			Conversation: ConversationPresentation{
				Slash:          "",
				Noun:           "会话",
				MenuLabel:      "会话管理",
				IDLabel:        "conversation id",
				IDPlaceholder:  "CONVERSATION_ID",
				SummaryLabel:   "conversation",
				HelpGroupLabel: "conversation",
			},
			Features: map[Feature]bool{},
		}
	}
}

// Supports reports whether the backend supports the given feature.
func (s CapabilitySpec) Supports(feature Feature) bool {
	return s.Features[feature]
}

// CurrentConversationLabel returns a localized label for the current conversation.
func (s CapabilitySpec) CurrentConversationLabel() string {
	return "当前" + s.Conversation.Noun
}

// MissingConversationLabel returns a localized label for when no conversation is active.
func (s CapabilitySpec) MissingConversationLabel() string {
	return "当前没有活动" + s.Conversation.Noun
}

// RewriteConversationText rewrites generic conversation text to backend-specific terminology.
func (s CapabilitySpec) RewriteConversationText(text string) string {
	if s.Kind != runtime.BackendClaude {
		return text
	}
	text = strings.ReplaceAll(text, "线程", s.Conversation.Noun)
	text = strings.ReplaceAll(text, "thread", s.Conversation.SummaryLabel)
	return text
}

// RewriteHelpEntries rewrites help entries to use backend-specific terminology.
func (s CapabilitySpec) RewriteHelpEntries(specs []menutypes.HelpCommandSpec) []menutypes.HelpCommandSpec {
	updated := make([]menutypes.HelpCommandSpec, 0, len(specs))
	for _, spec := range specs {
		item := spec
		item.Command = strings.ReplaceAll(item.Command, "/thread", s.Conversation.Slash)
		item.Command = strings.ReplaceAll(item.Command, "THREAD_ID", s.Conversation.IDPlaceholder)
		item.Summary = s.RewriteConversationText(item.Summary)
		updated = append(updated, item)
	}
	return updated
}

// HelpGroupLabel returns the backend-specific label for a help group.
func (s CapabilitySpec) HelpGroupLabel(group string) string {
	group = strings.TrimSpace(group)
	if group == "thread" {
		return s.Conversation.HelpGroupLabel
	}
	return group
}

// MenuGroupSpec rewrites a menu group spec for backend-specific terminology.
func (s CapabilitySpec) MenuGroupSpec(action string, spec menutypes.MenuGroupSpec) menutypes.MenuGroupSpec {
	if strings.TrimSpace(action) == "menu.thread" {
		spec.Label = s.Conversation.MenuLabel
		spec.Description = "查看当前" + s.Conversation.Noun + "状态，并通过下拉切换" + s.Conversation.Noun + "。"
	}
	return spec
}

// MenuNodeLabel returns the backend-specific label for a menu node.
func (s CapabilitySpec) MenuNodeLabel(action, label string) string {
	switch strings.TrimSpace(action) {
	case "menu.thread":
		return s.Conversation.MenuLabel
	case "thread.permission_mode.menu":
		if s.Supports(FeatureConversationPermissions) {
			return "会话权限"
		}
	case "workspace.permission_mode.menu":
		if s.Supports(FeatureWorkspacePermissions) {
			return "默认权限"
		}
	}
	return strings.TrimSpace(label)
}

// ConversationHelpEntries rewrites help entries for the given backend.
func ConversationHelpEntries(backend string, specs []menutypes.HelpCommandSpec) []menutypes.HelpCommandSpec {
	return ForKind(backend).RewriteHelpEntries(specs)
}
