package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"feidex/internal/app/appcore"
	"feidex/internal/app/appstate"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const (
	groupAnnouncementDefaultDebounce    = 2 * time.Second
	groupAnnouncementDefaultMinInterval = 15 * time.Second
	groupAnnouncementRefreshTimeout     = 20 * time.Second
)

type groupAnnouncementTracker struct {
	mu          sync.Mutex
	debounce    time.Duration
	minInterval time.Duration
	timers      map[string]*time.Timer
	running     map[string]bool
	queued      map[string]bool
	lastAttempt map[string]time.Time
}

func newGroupAnnouncementTracker() *groupAnnouncementTracker {
	return &groupAnnouncementTracker{
		debounce:    groupAnnouncementDefaultDebounce,
		minInterval: groupAnnouncementDefaultMinInterval,
		timers:      map[string]*time.Timer{},
		running:     map[string]bool{},
		queued:      map[string]bool{},
		lastAttempt: map[string]time.Time{},
	}
}

func scheduleGroupAnnouncementStatusRefresh(a *App, chatID, reason string) {
	if a == nil || a.trackers.groupAnnouncements == nil {
		return
	}
	a.trackers.groupAnnouncements.Schedule(a, chatID, reason)
}

func scheduleAllGroupAnnouncementStatusRefreshes(a *App, reason string) {
	for _, chatID := range knownGroupAnnouncementChatIDs(a) {
		scheduleGroupAnnouncementStatusRefresh(a, chatID, reason)
	}
}

func scheduleStartupGroupAnnouncementRefreshes(a *App) {
	scheduleAllGroupAnnouncementStatusRefreshes(a, "startup")
}

func (t *groupAnnouncementTracker) Schedule(a *App, chatID, reason string) {
	chatID = strings.TrimSpace(chatID)
	if t == nil || a == nil || chatID == "" {
		return
	}
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timers == nil {
		t.timers = map[string]*time.Timer{}
	}
	if t.running == nil {
		t.running = map[string]bool{}
	}
	if t.queued == nil {
		t.queued = map[string]bool{}
	}
	if t.lastAttempt == nil {
		t.lastAttempt = map[string]time.Time{}
	}
	if t.running[chatID] {
		t.queued[chatID] = true
		return
	}
	delay := t.debounce
	if delay < 0 {
		delay = 0
	}
	if last := t.lastAttempt[chatID]; !last.IsZero() && t.minInterval > 0 {
		if remaining := t.minInterval - now.Sub(last); remaining > delay {
			delay = remaining
		}
	}
	if timer := t.timers[chatID]; timer != nil {
		timer.Stop()
	}
	t.timers[chatID] = time.AfterFunc(delay, func() {
		t.Execute(a, chatID)
	})
	slog.Debug("group announcement refresh scheduled",
		"frontend_id", strings.TrimSpace(a.FrontendID()),
		"chat_id", chatID,
		"reason", strings.TrimSpace(reason),
		"delay_ms", delay.Milliseconds(),
	)
}

func (t *groupAnnouncementTracker) Execute(a *App, chatID string) {
	chatID = strings.TrimSpace(chatID)
	if t == nil || a == nil || chatID == "" {
		return
	}
	t.mu.Lock()
	if t.timers == nil {
		t.timers = map[string]*time.Timer{}
	}
	if t.running == nil {
		t.running = map[string]bool{}
	}
	if t.queued == nil {
		t.queued = map[string]bool{}
	}
	if t.lastAttempt == nil {
		t.lastAttempt = map[string]time.Time{}
	}
	if t.timers != nil {
		delete(t.timers, chatID)
	}
	t.running[chatID] = true
	t.queued[chatID] = false
	t.lastAttempt[chatID] = time.Now()
	t.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), groupAnnouncementRefreshTimeout)
	err := refreshGroupAnnouncementStatusNow(ctx, a, chatID)
	cancel()
	if err != nil {
		slog.Warn("group announcement refresh failed",
			"frontend_id", strings.TrimSpace(a.FrontendID()),
			"chat_id", chatID,
			"error", err,
		)
	}

	t.mu.Lock()
	queued := t.queued[chatID]
	t.running[chatID] = false
	t.queued[chatID] = false
	t.mu.Unlock()
	if queued {
		t.Schedule(a, chatID, "coalesced")
	}
}

func refreshGroupAnnouncementStatusNow(ctx context.Context, a *App, chatID string) error {
	chatID = strings.TrimSpace(chatID)
	if a == nil || a.feishu == nil || chatID == "" {
		return nil
	}
	status := buildGroupAnnouncementStatus(a, chatID, time.Now())
	if status.marker == "" || status.content == "" {
		return nil
	}
	st := a.State()
	if st == nil {
		return nil
	}
	record := st.GroupAnnouncementBlock("group", chatID)
	if record != nil && strings.TrimSpace(record.BlockID) != "" && strings.TrimSpace(record.LastContentHash) == status.stableHash {
		return nil
	}
	if record == nil {
		record = &state.GroupAnnouncementBlock{
			ID:         appstate.DefaultGroupAnnouncementBlockID(a.FrontendID(), "group", chatID),
			FrontendID: strings.TrimSpace(a.FrontendID()),
			ChatID:     chatID,
			ChatType:   "group",
		}
	}
	blockID := strings.TrimSpace(record.BlockID)
	if blockID == "" {
		blocks, err := a.feishu.ListAnnouncementBlocks(ctx, chatID)
		if err != nil {
			if feishu.IsAnnouncementRateLimit(err) {
				slog.Warn("group announcement refresh skipped by rate limit", "chat_id", chatID, "op", "list", "error", err)
				return nil
			}
			return err
		}
		blockID = findAnnouncementBlockID(blocks, groupAnnouncementMarkerCandidates(status)...)
	}
	if blockID == "" {
		created, err := a.feishu.CreateAnnouncementTextBlock(ctx, chatID, chatID, status.content, "")
		if err != nil {
			if feishu.IsAnnouncementRateLimit(err) {
				slog.Warn("group announcement refresh skipped by rate limit", "chat_id", chatID, "op", "create", "error", err)
				return nil
			}
			return err
		}
		blockID = strings.TrimSpace(created.BlockID)
	} else if err := a.feishu.UpdateAnnouncementTextBlock(ctx, chatID, blockID, status.content, ""); err != nil {
		if feishu.IsAnnouncementRateLimit(err) {
			slog.Warn("group announcement refresh skipped by rate limit", "chat_id", chatID, "op", "update", "error", err)
			return nil
		}
		return err
	}
	if blockID == "" {
		return nil
	}
	record.FrontendID = strings.TrimSpace(a.FrontendID())
	record.ChatID = chatID
	record.ChatType = "group"
	record.BotOpenID = status.botOpenID
	record.BlockID = blockID
	record.Marker = status.marker
	record.LastContentHash = status.stableHash
	record.LastUpdatedAt = status.updatedAt.Unix()
	return st.SaveGroupAnnouncementBlock(record)
}

type groupAnnouncementStatus struct {
	marker     string
	content    string
	stableHash string
	frontendID string
	botOpenID  string
	updatedAt  time.Time
}

func buildGroupAnnouncementStatus(a *App, chatID string, updatedAt time.Time) groupAnnouncementStatus {
	frontendID := firstNonEmpty(strings.TrimSpace(a.FrontendID()), config.DefaultFrontendID)
	botOpenID := groupAnnouncementBotOpenID(a, chatID)
	botName := groupAnnouncementBotName(a, botOpenID)
	marker := groupAnnouncementMarker(botName, botOpenID)
	stableLines := []string{
		marker,
		"Bot: " + botName,
		"Machine IP: " + firstNonEmpty(localAnnouncementMachineIP(), "unknown"),
		"Workspace: " + groupAnnouncementWorkspaceDir(a, chatID),
		"Backend: " + firstNonEmpty(configuredBackend(a), "unset"),
		"Thread: " + firstNonEmpty(groupAnnouncementThreadID(a, chatID), "none"),
	}
	stableContent := strings.Join(stableLines, "\n")
	content := stableContent + "\nUpdated: " + updatedAt.Format(time.RFC3339)
	return groupAnnouncementStatus{
		marker:     marker,
		content:    content,
		stableHash: hashGroupAnnouncementStableContent(stableContent),
		frontendID: frontendID,
		botOpenID:  botOpenID,
		updatedAt:  updatedAt,
	}
}

func groupAnnouncementBotOpenID(a *App, chatID string) string {
	if botOpenID := strings.TrimSpace(currentBotOpenID(a)); botOpenID != "" {
		return botOpenID
	}
	return strings.TrimSpace(groupPrimaryOwnerOpenID(a, "group", chatID))
}

func groupAnnouncementMarker(botName, botOpenID string) string {
	nameToken := groupAnnouncementMarkerToken(firstNonEmpty(strings.TrimSpace(botName), "bot"))
	idToken := groupAnnouncementMarkerToken(firstNonEmpty(strings.TrimSpace(botOpenID), "unknown"))
	return "feidex-status-region:" + firstNonEmpty(nameToken, "bot") + ":" + firstNonEmpty(idToken, "unknown")
}

func groupAnnouncementLegacyMarker(frontendID, botOpenID string) string {
	return "feidex-status-region:" + firstNonEmpty(strings.TrimSpace(frontendID), config.DefaultFrontendID) + ":" + firstNonEmpty(strings.TrimSpace(botOpenID), "unknown")
}

func groupAnnouncementMarkerToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		keep := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.'
		if keep {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func groupAnnouncementBotName(a *App, botOpenID string) string {
	if a != nil && a.feishu != nil {
		if name := strings.TrimSpace(a.feishu.BotName()); name != "" {
			return name
		}
	}
	return firstNonEmpty(strings.TrimSpace(botOpenID), "unknown")
}

func hashGroupAnnouncementStableContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func groupAnnouncementMarkerCandidates(status groupAnnouncementStatus) []string {
	markers := []string{strings.TrimSpace(status.marker)}
	frontendID := firstNonEmpty(strings.TrimSpace(status.frontendID), config.DefaultFrontendID)
	markers = append(markers,
		groupAnnouncementLegacyMarker(frontendID, status.botOpenID),
		groupAnnouncementLegacyMarker(frontendID, ""),
	)
	out := make([]string, 0, len(markers))
	seen := map[string]struct{}{}
	for _, marker := range markers {
		marker = strings.TrimSpace(marker)
		if marker == "" {
			continue
		}
		if _, ok := seen[marker]; ok {
			continue
		}
		seen[marker] = struct{}{}
		out = append(out, marker)
	}
	return out
}

func findAnnouncementBlockID(blocks []feishu.AnnouncementBlock, markers ...string) string {
	if len(markers) == 0 {
		return ""
	}
	for _, block := range blocks {
		for _, marker := range markers {
			marker = strings.TrimSpace(marker)
			if marker != "" && strings.Contains(block.Text, marker) && strings.TrimSpace(block.BlockID) != "" {
				return strings.TrimSpace(block.BlockID)
			}
		}
	}
	return ""
}

func groupAnnouncementWorkspaceDir(a *App, chatID string) string {
	if a == nil || strings.TrimSpace(chatID) == "" {
		return "unconfigured"
	}
	binding := agentBindingForChat(a, "group", chatID)
	if binding == nil || strings.TrimSpace(binding.WorkspaceID) == "" {
		return "unconfigured"
	}
	if ws := config.FindWorkspace(a.cfg, binding.WorkspaceID); ws != nil && strings.TrimSpace(ws.Cwd) != "" {
		return strings.TrimSpace(ws.Cwd)
	}
	return "unavailable:" + strings.TrimSpace(binding.WorkspaceID)
}

func groupAnnouncementThreadID(a *App, chatID string) string {
	if a == nil || strings.TrimSpace(chatID) == "" {
		return ""
	}
	var best *state.Session
	for _, sess := range a.State().Sessions() {
		if !sessionMatchesGroupChat(a, sess, chatID) || strings.TrimSpace(sess.ActiveThreadID) == "" {
			continue
		}
		if best == nil || sess.UpdatedAt > best.UpdatedAt {
			best = sess
		}
	}
	if best == nil {
		return ""
	}
	return strings.TrimSpace(best.ActiveThreadID)
}

func knownGroupAnnouncementChatIDs(a *App) []string {
	if a == nil || a.State() == nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, binding := range a.State().AgentBindings() {
		if binding == nil || strings.ToLower(strings.TrimSpace(binding.ChatType)) != "group" || strings.TrimSpace(binding.ChatID) == "" {
			continue
		}
		seen[strings.TrimSpace(binding.ChatID)] = struct{}{}
	}
	for _, sess := range a.State().Sessions() {
		if sess == nil {
			continue
		}
		chatID := strings.TrimSpace(sess.ChatID)
		if chatID == "" {
			_, _, chatID, _, _ = appcore.ParseSessionKey(sess.Key)
		}
		if !sessionMatchesGroupChat(a, sess, chatID) {
			continue
		}
		if chatID != "" {
			seen[chatID] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for chatID := range seen {
		out = append(out, chatID)
	}
	return out
}

func sessionMatchesGroupChat(a *App, sess *state.Session, chatID string) bool {
	chatID = strings.TrimSpace(chatID)
	if a == nil || sess == nil || chatID == "" || !appcore.SessionBelongsToFrontend(a, sess.Key) {
		return false
	}
	sessChatID := strings.TrimSpace(sess.ChatID)
	sessChatType := strings.ToLower(strings.TrimSpace(sess.ChatType))
	if sessChatID == "" || sessChatType == "" {
		_, keyChatType, keyChatID, _, _ := appcore.ParseSessionKey(sess.Key)
		if sessChatID == "" {
			sessChatID = keyChatID
		}
		if sessChatType == "" {
			sessChatType = keyChatType
		}
	}
	if sessChatID != chatID {
		return false
	}
	if sessChatType == "group" {
		return true
	}
	if sessChatType != "" {
		return false
	}
	return agentBindingForChat(a, "group", chatID) != nil || hasGroupPrimaryState(a, "group", chatID)
}

func localAnnouncementMachineIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil {
				return ipv4.String()
			}
		}
	}
	return ""
}
