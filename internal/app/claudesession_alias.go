package app

import "feidex/internal/app/claudesession"

// Function aliases — session catalog
var listClaudeSessions = claudesession.ListSessions
var findClaudeSessionEntry = claudesession.FindSessionEntry
var sanitizeClaudeProjectDirName = claudesession.SanitizeProjectDirName
