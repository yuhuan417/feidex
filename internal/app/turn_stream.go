package app

import (
	"context"
	"sort"
	"strings"

	"feidex/internal/state"
)

type turnStream struct {
	TurnID       string
	ThreadID     string
	SubmissionID string
	SessionKey   string
	WorkspaceID  string

	PendingPlan  string
	LastSentPlan string
	LastError    string
	SentOutput   bool
	SentFinal    bool
	NextOrder    int
	Items        map[string]*turnItemBuffer
}

type turnItemBuffer struct {
	Key      string
	ItemID   string
	ItemType string
	Command  string
	Delta    string
	Order    int
}

type turnItemSnapshot struct {
	ItemID        string
	ItemType      string
	StoreText     string
	SendText      string
	DetailText    string
	LinkKind      string
	IsOutput      bool
	Expandable    bool
	IsFinalAnswer bool
}

type turnStreamFlushResult struct {
	SentOutput bool
	SawFinal   bool
	LastError  string
}

func (a *App) noteTurnStarted(sessionKey string, sub *state.Submission) {
	if sub == nil || strings.TrimSpace(sub.TurnID) == "" {
		return
	}
	a.turnStreamsMu.Lock()
	defer a.turnStreamsMu.Unlock()
	a.ensureTurnStreamLocked(sessionKey, sub)
}

func (a *App) appendTurnItemDelta(threadID, turnID, itemID, itemType, delta string) {
	if strings.TrimSpace(delta) == "" {
		return
	}
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}

	a.turnStreamsMu.Lock()
	defer a.turnStreamsMu.Unlock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	key := turnItemKey(itemID, itemType)
	item := stream.ensureItemBuffer(key, itemID, itemType)
	item.Delta += delta
}

func (a *App) updatePendingPlan(turnID, plan string) {
	sessionKey, sub := a.findSubmissionByTurn("", turnID)
	if sub == nil {
		return
	}
	a.turnStreamsMu.Lock()
	defer a.turnStreamsMu.Unlock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	stream.PendingPlan = strings.TrimSpace(plan)
}

func (a *App) recordTurnError(threadID, turnID, message string) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	a.turnStreamsMu.Lock()
	defer a.turnStreamsMu.Unlock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	stream.LastError = strings.TrimSpace(message)
}

func (a *App) completeTurnItem(ctx context.Context, threadID, turnID, itemID string, item map[string]any) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}

	var (
		planText string
		snapshot turnItemSnapshot
	)

	a.turnStreamsMu.Lock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	if strings.TrimSpace(threadID) != "" {
		stream.ThreadID = threadID
	}
	if text := strings.TrimSpace(stream.PendingPlan); text != "" && text != stream.LastSentPlan {
		planText = text
		stream.LastSentPlan = text
		stream.PendingPlan = ""
	}
	itemType := normalizeTurnItemType(stringValue(item["type"]))
	key := turnItemKey(itemID, itemType)
	buf := stream.ensureItemBuffer(key, itemID, itemType)
	if command := firstNonEmpty(stringValue(item["command"]), stringValue(item["commandLine"])); command != "" {
		buf.Command = command
	}
	snapshot = snapshotTurnItem(buf, item, false)
	delete(stream.Items, key)
	if snapshot.IsFinalAnswer {
		stream.SentFinal = true
	}
	a.turnStreamsMu.Unlock()

	if planText != "" {
		a.storePlanSnapshot(sub.ID, planText)
		a.sendPlanCard(ctx, sub, planText, true)
	}
	a.deliverTurnItemSnapshot(ctx, sub, snapshot, true)
}

func (a *App) flushTurnStream(ctx context.Context, threadID, turnID string) turnStreamFlushResult {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		a.turnStreamsMu.Lock()
		delete(a.turnStreams, turnID)
		a.turnStreamsMu.Unlock()
		return turnStreamFlushResult{}
	}

	var (
		planText string
		items    []turnItemSnapshot
		result   turnStreamFlushResult
	)

	a.turnStreamsMu.Lock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	result.SentOutput = stream.SentOutput
	result.SawFinal = stream.SentFinal
	result.LastError = stream.LastError
	if text := strings.TrimSpace(stream.PendingPlan); text != "" && text != stream.LastSentPlan {
		planText = text
	}
	pending := make([]*turnItemBuffer, 0, len(stream.Items))
	for _, item := range stream.Items {
		pending = append(pending, item)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Order < pending[j].Order })
	for _, item := range pending {
		items = append(items, snapshotTurnItem(item, nil, true))
	}
	delete(a.turnStreams, turnID)
	a.turnStreamsMu.Unlock()

	if planText != "" {
		a.storePlanSnapshot(sub.ID, planText)
		a.sendPlanCard(ctx, sub, planText, false)
	}
	for _, item := range items {
		if sent := a.deliverTurnItemSnapshot(ctx, sub, item, false); sent && item.IsOutput {
			result.SentOutput = true
		}
	}
	return result
}

func (a *App) deliverTurnItemSnapshot(ctx context.Context, sub *state.Submission, snapshot turnItemSnapshot, includeActions bool) bool {
	if sub == nil {
		return false
	}
	a.storeTurnItemSnapshot(sub.ID, snapshot)
	if strings.TrimSpace(snapshot.SendText) == "" && strings.TrimSpace(snapshot.DetailText) == "" {
		return false
	}
	id := a.sendTurnSnapshotCard(ctx, sub, snapshot, includeActions)
	ids := []string{}
	if strings.TrimSpace(id) != "" {
		ids = append(ids, id)
	}
	if snapshot.IsOutput && len(ids) > 0 {
		a.turnStreamsMu.Lock()
		if stream := a.turnStreams[sub.TurnID]; stream != nil {
			stream.SentOutput = true
		}
		a.turnStreamsMu.Unlock()
	}
	return len(ids) > 0
}

func (a *App) storePlanSnapshot(submissionID, plan string) {
	if strings.TrimSpace(submissionID) == "" {
		return
	}
	_ = a.appState().updateSubmission(submissionID, func(sub *state.Submission) {
		sub.PlanText = strings.TrimSpace(plan)
	})
}

func (a *App) storeTurnItemSnapshot(submissionID string, snapshot turnItemSnapshot) {
	if strings.TrimSpace(submissionID) == "" || strings.TrimSpace(snapshot.StoreText) == "" {
		return
	}
	_ = a.appState().updateSubmission(submissionID, func(sub *state.Submission) {
		switch snapshot.ItemType {
		case "reasoning":
			sub.SummaryText = appendSeparatedText(sub.SummaryText, snapshot.StoreText)
		case "command_execution":
			sub.CommandText = appendSeparatedText(sub.CommandText, snapshot.StoreText)
		case "agent_message":
			sub.OutputText = appendSeparatedText(sub.OutputText, snapshot.StoreText)
		}
	})
}

func (a *App) ensureTurnStreamLocked(sessionKey string, sub *state.Submission) *turnStream {
	if a.turnStreams == nil {
		a.turnStreams = map[string]*turnStream{}
	}
	stream := a.turnStreams[sub.TurnID]
	if stream != nil {
		stream.SessionKey = sessionKey
		stream.SubmissionID = sub.ID
		stream.WorkspaceID = sub.WorkspaceID
		if strings.TrimSpace(sub.ThreadID) != "" {
			stream.ThreadID = sub.ThreadID
		}
		return stream
	}
	stream = &turnStream{
		TurnID:       sub.TurnID,
		ThreadID:     sub.ThreadID,
		SubmissionID: sub.ID,
		SessionKey:   sessionKey,
		WorkspaceID:  sub.WorkspaceID,
		Items:        map[string]*turnItemBuffer{},
	}
	a.turnStreams[sub.TurnID] = stream
	return stream
}

func (s *turnStream) ensureItemBuffer(key, itemID, itemType string) *turnItemBuffer {
	if s.Items == nil {
		s.Items = map[string]*turnItemBuffer{}
	}
	if item := s.Items[key]; item != nil {
		if item.ItemID == "" {
			item.ItemID = strings.TrimSpace(itemID)
		}
		if item.ItemType == "" {
			item.ItemType = normalizeTurnItemType(itemType)
		}
		return item
	}
	s.NextOrder++
	item := &turnItemBuffer{
		Key:      key,
		ItemID:   strings.TrimSpace(itemID),
		ItemType: normalizeTurnItemType(itemType),
		Order:    s.NextOrder,
	}
	s.Items[key] = item
	return item
}
