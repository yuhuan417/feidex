package app

import applinkutil "feidex/internal/app/linkutil"

const (
	markdownFenceMinLen       = applinkutil.FenceMinLen
	markdownFencePreferredLen = applinkutil.FencePreferredLen
)

var normalizeCardMarkdown = applinkutil.NormalizeCardMarkdown

var parseBacktickFenceLine = applinkutil.ParseBacktickFenceLine

var countLeadingBackticks = applinkutil.CountLeadingBackticks
