package app

import "feidex/internal/app/turnitem"

// Type aliases
type turnItemCardPayload = turnitem.CardPayload
type turnItemTracker = turnitem.Tracker
type turnItemState = turnitem.State
type jsonNumber = turnitem.JSONNumber

// Function aliases - formatting (production)
var normalizeTurnItemType = turnitem.NormalizeTurnItemType
var prettyJSON = turnitem.PrettyJSON
var stringValue = turnitem.StringValue
var markdownCodeBlock = turnitem.MarkdownCodeBlock
var markdownCodeBlockWithLang = turnitem.MarkdownCodeBlockWithLang
var inlineCodeText = turnitem.InlineCodeText
var markdownInlineCode = turnitem.MarkdownInlineCode
var trimmedNonEmptyStrings = turnitem.TrimmedNonEmptyStrings

// Function aliases - card (production)
var isReplyTurnItem = turnitem.IsReplyTurnItem
var replyTurnItemCardBody = turnitem.ReplyTurnItemCardBody
var replyTurnItemCardTitle = turnitem.ReplyTurnItemCardTitle
var compactTurnItemCardContent = turnitem.CompactTurnItemCardContent
var turnItemCardMeta = turnitem.TurnItemCardMeta
var turnItemEventKind = turnitem.TurnItemEventKind

// Function aliases - payload (production)
var buildTurnItemCardPayloadWithWorkspace = turnitem.BuildTurnItemCardPayload

// Function aliases - state (production)
var turnItemStateKey = turnitem.StateKey
var cloneJSONMap = turnitem.CloneJSONMap
var mergeJSONMaps = turnitem.MergeJSONMaps
var newTurnItemTracker = turnitem.NewTracker

// Function aliases - dynamic tool (production)
var buildClaudeQuietDynamicToolLines = turnitem.BuildClaudeQuietDynamicToolLines
var quietDisplayFileName = turnitem.QuietDisplayFileName
var buildQuietSearchLine = turnitem.BuildQuietSearchLine
