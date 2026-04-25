package app

// download command, file sharing workflow, and related helpers are now provided
// by the debugviewcmd sub-package via debugviewcmd_adapters.go.
//
// The following aliases are defined in debugviewcmd_adapters.go:
//   - commandDownload
//   - completeMenuDownload
//   - completeDownloadFileConfirm
//   - finishDownloadFileShare
//   - renderDownloadPreparingCard
//   - renderDownloadReadyCard
//   - renderDownloadFailedCard
//   - renderDownloadDisplayPath
//   - formatDownloadSize
//   - newDownloadPathPickerPayload
//   - downloadFilePendingKind
