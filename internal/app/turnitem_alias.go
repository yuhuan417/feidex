package app

import "feidex/internal/app/turnitem"

// Type aliases
type turnItemCardPayload = turnitem.CardPayload
type turnItemTracker = turnitem.Tracker
type turnItemState = turnitem.State
type claudeDynamicToolCardTemplate = turnitem.DynamicToolCardTemplate
type claudeDynamicToolCategory = turnitem.DynamicToolCategory
type jsonNumber = turnitem.JSONNumber

// Constant aliases
const claudeDynamicToolUnknownCategory = turnitem.DynamicToolUnknownCategory
const claudeDynamicToolCodeCategory = turnitem.DynamicToolCodeCategory
const claudeDynamicToolShellCategory = turnitem.DynamicToolShellCategory
const claudeDynamicToolWebCategory = turnitem.DynamicToolWebCategory
const claudeDynamicToolTodoCategory = turnitem.DynamicToolTodoCategory
const claudeDynamicToolPlanCategory = turnitem.DynamicToolPlanCategory
const claudeDynamicToolTaskCategory = turnitem.DynamicToolTaskCategory
const claudeDynamicToolSkillCategory = turnitem.DynamicToolSkillCategory
const claudeDynamicToolMCPCategory = turnitem.DynamicToolMCPCategory

// Function aliases - formatting
var normalizeTurnItemType = turnitem.NormalizeTurnItemType
var turnItemLabel = turnitem.TurnItemLabel
var extractTurnItemText = turnitem.ExtractTurnItemText
var isCodeStyledTurnItem = turnitem.IsCodeStyledTurnItem
var intValue = turnitem.IntValue
var optionalIntPointer = turnitem.OptionalIntPointer
var prettyJSON = turnitem.PrettyJSON
var stringValue = turnitem.StringValue
var markdownCodeBlock = turnitem.MarkdownCodeBlock
var markdownCodeBlockWithLang = turnitem.MarkdownCodeBlockWithLang
var inlineCodeText = turnitem.InlineCodeText
var markdownInlineCode = turnitem.MarkdownInlineCode
var maxConsecutiveBackticks = turnitem.MaxConsecutiveBackticks
var trimmedNonEmptyStrings = turnitem.TrimmedNonEmptyStrings

// Function aliases - card
var isReplyTurnItem = turnitem.IsReplyTurnItem
var replyTurnItemCardBody = turnitem.ReplyTurnItemCardBody
var replyTurnItemCardTitle = turnitem.ReplyTurnItemCardTitle
var compactTurnItemCardContent = turnitem.CompactTurnItemCardContent
var stripTurnItemCardHeading = turnitem.StripTurnItemCardHeading
var splitCompactMetaLine = turnitem.SplitCompactMetaLine
var joinMarkdownSections = turnitem.JoinMarkdownSections
var turnItemCardMeta = turnitem.TurnItemCardMeta
var turnItemEventKind = turnitem.TurnItemEventKind

// Function aliases - payload
var buildTurnItemCardPayloadWithWorkspace = turnitem.BuildTurnItemCardPayload
var buildLabeledTurnEventText = turnitem.BuildLabeledTurnEventText
var summarizeCommandExecution = turnitem.SummarizeCommandExecution
var formatTurnCommandOutput = turnitem.FormatTurnCommandOutput
var summarizeFileChangeItem = turnitem.SummarizeFileChangeItem
var summarizeGenericTurnItem = turnitem.SummarizeGenericTurnItem
var summarizeToolCallSummaryLines = turnitem.SummarizeToolCallSummaryLines
var summarizeToolCallStatusLine = turnitem.SummarizeToolCallStatusLine
var summarizeToolCallDetail = turnitem.SummarizeToolCallDetail
var summarizeToolInputLines = turnitem.SummarizeToolInputLines
var summarizeTodoInputLines = turnitem.SummarizeTodoInputLines
var summarizeToolInputFallbackLines = turnitem.SummarizeToolInputFallbackLines
var toolInputMap = turnitem.ToolInputMap
var toolInputSequence = turnitem.ToolInputSequence
var limitSummaryLines = turnitem.LimitSummaryLines
var looksLikePathKey = turnitem.LooksLikePathKey

// Function aliases - state
var turnItemStateKey = turnitem.StateKey
var cloneJSONMap = turnitem.CloneJSONMap
var cloneJSONValue = turnitem.CloneJSONValue
var mergeJSONMaps = turnitem.MergeJSONMaps
var newTurnItemTracker = turnitem.NewTracker

// Function aliases - dynamic tool
var classifyClaudeDynamicTool = turnitem.ClassifyDynamicTool
var buildClaudeDynamicToolCardTemplate = turnitem.BuildClaudeDynamicToolCardTemplate
var buildClaudeQuietDynamicToolLines = turnitem.BuildClaudeQuietDynamicToolLines
var claudeDisplayToolName = turnitem.DisplayToolName
var quietDisplayFileName = turnitem.QuietDisplayFileName
var buildQuietSearchLine = turnitem.BuildQuietSearchLine
