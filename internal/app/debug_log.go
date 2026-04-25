package app

// debugService and related types/functions are now provided by the
// debugviewcmd sub-package via debugviewcmd_adapters.go.
//
// Callers should use the exported method names directly:
//   - CommandDebug, CommandDebugLogs
//   - CompleteMenuDebug, CompleteMenuDebugLogs
//   - DebugAccessAllowed
//   - RenderDebugAccessDeniedCard, RenderDebugLogsCard
//   - SetRuntimeDebug
