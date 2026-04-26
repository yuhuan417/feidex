package app

import "feidex/internal/app/turn"

// Type aliases — quiet card types from turn package
type quietWorkingCard = turn.QuietWorkingCard
type quietWorkingCardOp = turn.QuietWorkingCardOp
type quietWorkingBoundary = turn.QuietWorkingBoundary

// Constant aliases
const quietWorkingCardTitle = turn.QuietWorkingCardTitle
const quietWorkingCardColor = turn.QuietWorkingCardColor
const quietWorkingReasoningKey = turn.QuietWorkingReasoningKey

// Function aliases — quiet card formatting
var isQuietBoundaryTurnItem = turn.IsQuietBoundaryTurnItem
var buildQuietWorkingCardLines = turn.BuildWorkingCardLines
var quietWorkingItemKey = turn.WorkingItemKey
var compactQuietWorkingLines = turn.CompactQuietWorkingLines
var parseQuietMergeableLine = turn.ParseQuietMergeableLine
var normalizeWorkingStatus = turn.NormalizeWorkingStatus
var normalizeCommandActionType = turn.NormalizeCommandActionType
var normalizeWebSearchActionType = turn.NormalizeWebSearchActionType
var quietPatchChangeType = turn.QuietPatchChangeType
var quietPatchMovePath = turn.QuietPatchMovePath
var markdownInlineCodeSlice = turn.MarkdownInlineCodeSlice
var quietDedupeStrings = turn.QuietDedupeStrings
var equalStringSlices = turn.EqualStringSlices
var joinQuietStringList = turn.JoinQuietStringList
var quietWorkingEntryKey = turn.EntryKey
var quietWorkingEntryPrefix = turn.EntryPrefix
var buildQuietWebSearchLines = turn.BuildQuietWebSearchLines
var buildQuietCommandExecutionLines = turn.BuildQuietCommandExecutionLines
var compactQuietWorkingLinesWithDedup = turn.CompactQuietWorkingLinesWithDedup
