package app

import (
	appconvbackend "feidex/internal/app/convbackend"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

// Type aliases — convbackend sub-package
type conversationThreadsCardView = appconvbackend.ConversationThreadsCardView

// Function aliases — convbackend sub-package
var (
	buildConversationThreadsCard          = appconvbackend.BuildConversationThreadsCard
	renderCodexThreadsCard                = appconvbackend.RenderCodexThreadsCard
	renderClaudeThreadsCardForCurrentBackend = appconvbackend.RenderClaudeThreadsCardForCurrentBackend
	renderClaudeThreadsCard               = appconvbackend.RenderClaudeThreadsCard
)

// Suppress unused import warnings for types referenced by aliases.
var (
	_ = codexrpc.ThreadListEntry{}
	_ = config.Workspace{}
	_ = feishu.Button{}
)
