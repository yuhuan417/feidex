package app

// Reply continuation, message link tracking, and steer-inbound-reply logic
// has been extracted to internal/app/replycontinuation/.
//
// The replyContinuationService wrapper and newReplyContinuationService
// constructor are defined in replycontinuation_adapters.go.
//
// The staged image helpers (stagedImageAttachments, stagedImageSourceMessageIDs,
// stagedImageRootMessageIDs, sourceMessageIDsForSubmission) are re-exported
// from the submission package via the same adapter file.
