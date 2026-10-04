package feishuapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/domain/conversation"
	"feidex/internal/textutil"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"feidex/internal/application/announcement"
	"feidex/internal/config"
)

const (
	groupAnnouncementDefaultDebounce    = 2 * time.Second
	groupAnnouncementDefaultMinInterval = 15 * time.Second
	groupAnnouncementRefreshTimeout     = 20 * time.Second
	groupAnnouncementDivider            = "----------------------------------------"
	groupAnnouncementFieldWidth         = 10
	groupAnnouncementCommonChatType     = "group_common"
	groupAnnouncementCommonMarker       = "feidex-status-common-region"
	groupAnnouncementCommonTitle        = "Feidex Group Status"
)

func scheduleGroupAnnouncementStatusRefresh(a *App, chatID, reason string) {
	if a == nil || a.runtimeOwner == nil || a.runtimeOwner.Announcements == nil {
		return
	}
	a.runtimeOwner.Announcements.Schedule(chatID)
}

func GroupAnnouncementRefresh(a *App) func(context.Context, string) error {
	return func(ctx context.Context, chatID string) error {
		return refreshGroupAnnouncementStatusNow(ctx, a, chatID)
	}
}

// markGroupAnnouncementBotAbsent records that Feishu reports the app is no
// longer a member of chatID, so later refreshes skip it instead of retrying a
// request that cannot succeed. Cleared by clearGroupAnnouncementBotAbsent when
// the bot is added back.
func clearGroupAnnouncementBotAbsent(announcements announcement.Service, chatID string) {
	if strings.TrimSpace(chatID) == "" {
		return
	}
	if err := announcements.SetAbsent(chatID, false); err != nil {
		slog.Warn("clear announcement membership failed", "error", err)
	}
}

func scheduleAllGroupAnnouncementStatusRefreshes(a *App, reason string) {
	for _, chatID := range knownGroupAnnouncementChatIDs(a.bindings.AnnouncementQuery) {
		scheduleGroupAnnouncementStatusRefresh(a, chatID, reason)
	}
}

func scheduleStartupGroupAnnouncementRefreshes(a *App) {
	scheduleAllGroupAnnouncementStatusRefreshes(a, "startup")
}

func refreshGroupAnnouncementStatusNow(ctx context.Context, a *App, chatID string) error {
	if a == nil || a.feishu == nil {
		return nil
	}
	now := time.Now()
	status, common := buildGroupAnnouncementStatus(a, chatID, now), buildGroupAnnouncementCommonStatus(a, chatID, now)
	return a.bindings.Announcements.Refresh(ctx, chatID, status.applicationStatus(), common.applicationStatus())
}
func (s groupAnnouncementStatus) applicationStatus() announcement.Status {
	return announcement.NewStatus(s.marker, s.content, s.stableContent, s.stableHash, s.botOpenID, s.updatedAt)
}

type groupAnnouncementStatus struct {
	marker        string
	content       string
	stableContent string
	stableHash    string
	frontendID    string
	botOpenID     string
	updatedAt     time.Time
}

func buildGroupAnnouncementCommonStatus(a *App, chatID string, updatedAt time.Time) groupAnnouncementStatus {
	botOpenID := currentBotOpenID(a.feishu)
	stableLines := []string{
		groupAnnouncementCommonTitle,
		groupAnnouncementField("Primary Bot", currentBotDisplayName(a)),
		groupAnnouncementField("Marker", groupAnnouncementCommonMarker),
	}
	stableContent := strings.Join(stableLines, "\n")
	content := stableContent + "\n" + groupAnnouncementField("Updated", updatedAt.Format(time.RFC3339))
	return groupAnnouncementStatus{
		marker:        groupAnnouncementCommonMarker,
		content:       content,
		stableContent: stableContent,
		stableHash:    hashGroupAnnouncementStableContent(stableContent),
		botOpenID:     botOpenID,
		updatedAt:     updatedAt,
	}
}

func buildGroupAnnouncementStatus(a *App, chatID string, updatedAt time.Time) groupAnnouncementStatus {
	frontendID := textutil.FirstNonEmpty(strings.TrimSpace(a.FrontendID()), config.DefaultFrontendID)
	botOpenID := groupAnnouncementBotOpenID(a, chatID)
	botName := groupAnnouncementBotName(a.feishu, botOpenID)
	marker := groupAnnouncementMarker(botName, botOpenID)
	stableLines := []string{
		groupAnnouncementDivider,
		groupAnnouncementField("Bot", botName),
		groupAnnouncementField("Machine IP", textutil.FirstNonEmpty(localAnnouncementMachineIP(), "unknown")),
		groupAnnouncementField("Workspace", groupAnnouncementWorkspaceDir(a.bindings.AnnouncementQuery, chatID)),
		groupAnnouncementField("Backend", textutil.FirstNonEmpty(a.configView().configuredBackend(), "unset")),
		groupAnnouncementField("Thread", textutil.FirstNonEmpty(groupAnnouncementThreadID(a.bindings.ConversationQuery, chatID), "none")),
		groupAnnouncementField("Marker", marker),
	}
	stableContent := strings.Join(stableLines, "\n")
	content := stableContent + "\n" + groupAnnouncementField("Updated", updatedAt.Format(time.RFC3339))
	return groupAnnouncementStatus{
		marker:        marker,
		content:       content,
		stableContent: stableContent,
		stableHash:    hashGroupAnnouncementStableContent(stableContent),
		frontendID:    frontendID,
		botOpenID:     botOpenID,
		updatedAt:     updatedAt,
	}
}

func groupAnnouncementField(key, value string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "Field"
	}
	value = strings.TrimSpace(value)
	if len(key) >= groupAnnouncementFieldWidth {
		return key + ": " + value
	}
	return fmt.Sprintf("%-*s: %s", groupAnnouncementFieldWidth, key, value)
}

func groupAnnouncementBotOpenID(a *App, chatID string) string {
	_ = chatID
	return strings.TrimSpace(currentBotOpenID(a.feishu))
}

func groupAnnouncementMarker(botName, botOpenID string) string {
	nameToken := groupAnnouncementMarkerToken(textutil.FirstNonEmpty(strings.TrimSpace(botName), "bot"))
	idToken := groupAnnouncementMarkerToken(textutil.FirstNonEmpty(strings.TrimSpace(botOpenID), "unknown"))
	return "feidex-status-region:" + textutil.FirstNonEmpty(nameToken, "bot") + ":" + textutil.FirstNonEmpty(idToken, "unknown")
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

func groupAnnouncementBotName(client FeishuClient, botOpenID string) string {
	if client != nil {
		if name := strings.TrimSpace(client.BotName()); name != "" {
			return name
		}
	}
	return textutil.FirstNonEmpty(strings.TrimSpace(botOpenID), "unknown")
}

func hashGroupAnnouncementStableContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func groupAnnouncementWorkspaceDir(query announcement.Query, chatID string) string {
	return query.WorkspaceDirectory(chatID)
}

func groupAnnouncementThreadID(query conversationapp.Query, chatID string) string {
	if strings.TrimSpace(chatID) == "" {
		return ""
	}
	best := query.LatestGroupSession(chatID, "")
	if best == nil {
		return ""
	}
	return strings.TrimSpace(best.ActiveThreadID)
}

func knownGroupAnnouncementChatIDs(announcementquery announcement.Query) []string {
	return announcementquery.Chats()
}
func sessionMatchesGroupChat(announcementquery announcement.Query, sess *conversation.Session, chatID string) bool {
	return announcementquery.GroupSession(sess, chatID)
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
