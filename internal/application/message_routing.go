package application

import "strings"

// GroupPolicyRootMessageID normalizes the root anchor used by group routing.
// A root message does not reply to itself; descendants retain the root ID so
// policy and message-link lookup can share one identity.
func GroupPolicyRootMessageID(messageID, rootMessageID, parentMessageID string) string {
	rootMessageID = strings.TrimSpace(rootMessageID)
	if rootMessageID == "" {
		return ""
	}
	if strings.TrimSpace(parentMessageID) == "" && rootMessageID == strings.TrimSpace(messageID) {
		return ""
	}
	return rootMessageID
}
