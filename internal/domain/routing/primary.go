// Package routing contains pure routing policy for frontend and group
// assignment. Persistence and Feishu message decoding stay outside this
// package.
package routing

import "strings"

// GroupPrimaryAssignment is the semantic result of a valid primary command.
type GroupPrimaryAssignment struct {
	TargetBotOpenID string
}

// GroupPrimaryState is the domain representation of one frontend's primary
// assignment in one group. Storage adapters may map this to their snapshot
// DTOs without exposing those DTOs to routing use cases.
type GroupPrimaryState struct {
	FrontendID              string
	ChatID                  string
	ChatType                string
	Enabled                 bool
	LastAssignmentMessageID string
	LastAssignmentCreatedAt int64
}

// AssignmentStamp identifies the group message which changed primary.
// A nil stamp means a direct local update without assignment metadata.
type AssignmentStamp struct {
	MessageID string
	CreatedAt int64
}

// WithPrimary returns a detached new state. Replayed or older assignment
// messages leave the existing state untouched.
func (s GroupPrimaryState) WithPrimary(enabled bool, assignment *AssignmentStamp) (GroupPrimaryState, bool) {
	if assignment != nil {
		if StaleAssignment(s.LastAssignmentMessageID, s.LastAssignmentCreatedAt, assignment.MessageID, assignment.CreatedAt) {
			return s, false
		}
		s.LastAssignmentMessageID = strings.TrimSpace(assignment.MessageID)
		s.LastAssignmentCreatedAt = assignment.CreatedAt
	}
	s.Enabled = enabled
	return s, true
}

// ParseGroupPrimaryAssignment recognizes the only assignment forms that may
// change a frontend's local primary state: exactly one bot mention together
// with /primary on (or a bare mention of that bot).
func ParseGroupPrimaryAssignment(text string, mentionedOpenIDs []string) (GroupPrimaryAssignment, bool) {
	target := SingleMentionedOpenID(mentionedOpenIDs)
	if target == "" || CountMentionedOpenIDs(mentionedOpenIDs) != 1 {
		return GroupPrimaryAssignment{}, false
	}
	if ParsePrimaryOnCommand(text) || ParseEmptyBotMention(text) {
		return GroupPrimaryAssignment{TargetBotOpenID: target}, true
	}
	return GroupPrimaryAssignment{}, false
}

// ParsePrimaryOnCommand reports whether text is a valid /primary on command,
// including the /workspace /primary on form used by the shared command path.
func ParsePrimaryOnCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 || !strings.EqualFold(strings.TrimSpace(fields[len(fields)-1]), "on") {
		return false
	}
	prefix := fields[:len(fields)-2]
	switch strings.TrimSpace(fields[len(fields)-2]) {
	case "/primary":
	case "primary":
		if len(prefix) == 0 || strings.TrimSpace(prefix[len(prefix)-1]) != "/workspace" {
			return false
		}
		prefix = prefix[:len(prefix)-1]
	default:
		return false
	}
	for _, field := range prefix {
		if !strings.HasPrefix(strings.TrimSpace(field), "@") {
			return false
		}
	}
	return true
}

// ParseEmptyBotMention reports whether text consists solely of bot mentions.
func ParseEmptyBotMention(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		if !strings.HasPrefix(strings.TrimSpace(field), "@") {
			return false
		}
	}
	return true
}

// CountMentionedOpenIDs counts non-empty mentions.
func CountMentionedOpenIDs(mentionedOpenIDs []string) int {
	count := 0
	for _, openID := range mentionedOpenIDs {
		if strings.TrimSpace(openID) != "" {
			count++
		}
	}
	return count
}

// SingleMentionedOpenID returns the only non-empty mention, or "" when the
// input contains zero or more than one distinct mention.
func SingleMentionedOpenID(mentionedOpenIDs []string) string {
	var target string
	for _, openID := range mentionedOpenIDs {
		value := strings.TrimSpace(openID)
		if value == "" {
			continue
		}
		if target != "" {
			return ""
		}
		target = value
	}
	return target
}

// StaleAssignment reports whether an incoming assignment is older than the
// assignment already persisted for the same frontend and group.
func StaleAssignment(lastMessageID string, lastCreatedAt int64, messageID string, createdAt int64) bool {
	if strings.TrimSpace(messageID) != "" && strings.TrimSpace(messageID) == strings.TrimSpace(lastMessageID) {
		return true
	}
	if createdAt == 0 || lastCreatedAt == 0 {
		return false
	}
	return createdAt < lastCreatedAt
}
