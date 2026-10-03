// Package history defines transcript records presented independently of backend transport.
package history

type Record struct {
	EntryID, EntryType, Timestamp, PromptID, MessageID, ParentUUID, StopReason, Preview string
	Details                                                                             []string
}
