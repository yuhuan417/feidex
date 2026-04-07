package app

import (
	"context"
	"strings"

	"feidex/internal/config"
	"feidex/internal/state"
)

const feishuTextChunkLimit = 2800

func (a *App) sendFinalMessages(ctx context.Context, sub *state.Submission, text string, inThread bool) []string {
	return a.sendReplyMessages(ctx, sub, text, inThread, "final_message")
}

func (a *App) sendTurnEventMessages(ctx context.Context, sub *state.Submission, text string, inThread bool, kind string) []string {
	return a.sendReplyMessages(ctx, sub, text, inThread, kind)
}

func (a *App) sendReplyMessages(ctx context.Context, sub *state.Submission, text string, inThread bool, kind string) []string {
	if sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
		text = sanitizeLocalMarkdownLinks(text, ws.Cwd)
	}
	chunks := splitFeishuText(strings.TrimSpace(text), feishuTextChunkLimit)
	if len(chunks) == 0 {
		chunks = []string{"任务已结束。"}
	}
	var ids []string
	for _, chunk := range chunks {
		id, err := a.feishu.ReplyTextWithID(ctx, sub.TriggerMessageID, chunk, inThread)
		if err != nil {
			continue
		}
		if id != "" {
			ids = append(ids, id)
			_ = a.store.UpsertMessageLink(&state.MessageLink{
				MessageID:    id,
				Kind:         kind,
				SessionKey:   sub.SessionKey,
				SubmissionID: sub.ID,
				ThreadID:     sub.ThreadID,
				TurnID:       sub.TurnID,
			})
		}
	}
	if len(ids) > 0 {
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
			s.FinalMessageIDs = append([]string(nil), ids...)
		})
	}
	return ids
}

func splitFeishuText(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	sections := splitSections(text)
	chunks := make([]string, 0, len(sections))
	var current strings.Builder
	appendCurrent := func() {
		if current.Len() == 0 {
			return
		}
		chunks = append(chunks, strings.TrimSpace(current.String()))
		current.Reset()
	}
	for _, section := range sections {
		parts := splitSection(section, limit)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if current.Len() == 0 {
				current.WriteString(part)
				continue
			}
			candidate := current.String() + "\n\n" + part
			if len(candidate) > limit {
				appendCurrent()
				current.WriteString(part)
				continue
			}
			current.WriteString("\n\n")
			current.WriteString(part)
		}
	}
	appendCurrent()
	return chunks
}

func (a *App) recordMessageLink(messageID, kind string, sub *state.Submission, requestID string) {
	if strings.TrimSpace(messageID) == "" {
		return
	}
	link := &state.MessageLink{
		MessageID: messageID,
		Kind:      kind,
		RequestID: requestID,
	}
	if sub != nil {
		link.SessionKey = sub.SessionKey
		link.SubmissionID = sub.ID
		link.ThreadID = sub.ThreadID
		link.TurnID = sub.TurnID
	}
	_ = a.store.UpsertMessageLink(link)
}

func splitSections(text string) []string {
	lines := strings.Split(text, "\n")
	sections := []string{}
	var current strings.Builder
	inCode := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if current.Len() > 0 && !inCode {
				sections = append(sections, strings.TrimSpace(current.String()))
				current.Reset()
			}
			if current.Len() > 0 {
				current.WriteString("\n")
			}
			current.WriteString(line)
			inCode = !inCode
			if !inCode {
				sections = append(sections, strings.TrimSpace(current.String()))
				current.Reset()
			}
			continue
		}
		if !inCode && trimmed == "" {
			if current.Len() > 0 {
				sections = append(sections, strings.TrimSpace(current.String()))
				current.Reset()
			}
			continue
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		sections = append(sections, strings.TrimSpace(current.String()))
	}
	return sections
}

func splitSection(section string, limit int) []string {
	if len(section) <= limit {
		return []string{section}
	}
	if strings.HasPrefix(strings.TrimSpace(section), "```") {
		return splitCodeBlock(section, limit)
	}
	return splitPlainText(section, limit)
}

func splitPlainText(section string, limit int) []string {
	lines := strings.Split(section, "\n")
	chunks := []string{}
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		chunks = append(chunks, strings.TrimSpace(current.String()))
		current.Reset()
	}
	for _, line := range lines {
		line = strings.TrimRight(line, " ")
		if len(line) > limit {
			flush()
			for len(line) > limit {
				chunks = append(chunks, strings.TrimSpace(line[:limit]))
				line = line[limit:]
			}
			if strings.TrimSpace(line) != "" {
				current.WriteString(line)
			}
			continue
		}
		if current.Len() == 0 {
			current.WriteString(line)
			continue
		}
		candidate := current.String() + "\n" + line
		if len(candidate) > limit {
			flush()
			current.WriteString(line)
			continue
		}
		current.WriteString("\n")
		current.WriteString(line)
	}
	flush()
	return chunks
}

func splitCodeBlock(section string, limit int) []string {
	lines := strings.Split(section, "\n")
	if len(lines) < 2 {
		return splitPlainText(section, limit)
	}
	fence := lines[0]
	trailer := "```"
	content := lines[1:]
	if strings.TrimSpace(content[len(content)-1]) == "```" {
		content = content[:len(content)-1]
	}
	maxBody := limit - len(fence) - len(trailer) - 4
	if maxBody <= 0 {
		return splitPlainText(section, limit)
	}
	chunks := []string{}
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		chunks = append(chunks, fence+"\n"+current.String()+"\n"+trailer)
		current.Reset()
	}
	for _, line := range content {
		if len(line) > maxBody {
			flush()
			for len(line) > maxBody {
				chunks = append(chunks, fence+"\n"+line[:maxBody]+"\n"+trailer)
				line = line[maxBody:]
			}
			if line != "" {
				current.WriteString(line)
			}
			continue
		}
		if current.Len() == 0 {
			current.WriteString(line)
			continue
		}
		candidate := current.String() + "\n" + line
		if len(candidate) > maxBody {
			flush()
			current.WriteString(line)
			continue
		}
		current.WriteString("\n")
		current.WriteString(line)
	}
	flush()
	return chunks
}
