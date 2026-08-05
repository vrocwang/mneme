package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// WhatsAppData provides tools for parsing and analyzing exported WhatsApp chat data.
type WhatsAppData struct {
	workspace string
}

// NewWhatsAppData creates a WhatsApp data tool.
func NewWhatsAppData(workspace string) *WhatsAppData {
	return &WhatsAppData{workspace: workspace}
}

func (t *WhatsAppData) Schema() Schema {
	return Schema{
		Name:        "whatsapp_parse_chat",
		Description: "Parse an exported WhatsApp chat .txt file. Extracts messages, participants, dates, media references, and provides statistics. Supports both iOS and Android export formats.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the WhatsApp exported chat .txt file",
				},
				"action": map[string]interface{}{
					"type":        "string",
					"description": "What to do with the chat data: parse (extract all messages), stats (summary statistics), search (find specific messages), participants (list participants), timeline (date-based activity)",
					"enum":        []string{"parse", "stats", "search", "participants", "timeline"},
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query text (for search action)",
				},
				"participant": map[string]interface{}{
					"type":        "string",
					"description": "Filter by participant name (for parse/stats/timeline actions)",
				},
				"date_from": map[string]interface{}{
					"type":        "string",
					"description": "Start date filter (YYYY-MM-DD format)",
				},
				"date_to": map[string]interface{}{
					"type":        "string",
					"description": "End date filter (YYYY-MM-DD format)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Max messages to return. Default: 100.",
				},
			},
			"required": []string{"file_path", "action"},
		},
	}
}

func (t *WhatsAppData) Execute(ctx context.Context, args map[string]interface{}) Result {
	filePath, _ := args["file_path"].(string)
	action, _ := args["action"].(string)

	if filePath == "" || action == "" {
		return Result{Error: "file_path and action are required"}
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return Result{Error: fmt.Sprintf("read file: %v", err)}
	}

	messages := parseWhatsAppChat(string(data))
	if len(messages) == 0 {
		return Result{Error: "no messages found in file. Ensure it's a valid WhatsApp export (iOS or Android format)."}
	}

	// Apply filters
	filterParticipant, _ := args["participant"].(string)
	dateFrom, _ := args["date_from"].(string)
	dateTo, _ := args["date_to"].(string)
	limit := toInt(args["limit"])

	if limit == 0 {
		limit = 100
	}

	messages = filterMessages(messages, filterParticipant, dateFrom, dateTo)

	switch action {
	case "parse":
		return t.parseAction(messages, limit)
	case "stats":
		return t.statsAction(messages, filterParticipant, dateFrom, dateTo)
	case "search":
		query, _ := args["query"].(string)
		return t.searchAction(messages, query, limit)
	case "participants":
		return t.participantsAction(messages)
	case "timeline":
		return t.timelineAction(messages)
	default:
		return Result{Error: fmt.Sprintf("unknown action: %s", action)}
	}
}

func (t *WhatsAppData) PermissionLevel() PermissionLevel { return PermReadOnly }
func (t *WhatsAppData) SideEffects() bool                { return false }
func (t *WhatsAppData) ConcurrencySafe() bool            { return true }
func (t *WhatsAppData) MaxResultChars() int              { return 10000 }

// ── Message parsing ───────────────────────────────────────────────────

// WhatsAppMessage represents a single parsed WhatsApp message.
type WhatsAppMessage struct {
	Timestamp   time.Time
	Sender      string
	Content     string
	IsSystem    bool
	HasMedia    bool
	MediaType   string // "image", "video", "audio", "document", "sticker", "location", ""
	IsForwarded bool
}

// iOS format: [01/01/2024, 10:30:00] Sender Name: Message content
var iosFormat = regexp.MustCompile(`^\[(\d{1,2}/\d{1,2}/\d{4}, \d{1,2}:\d{2}:\d{2}(?: [AP]M)?)\] ([^:]+): (.+)$`)

// Android format: 01/01/2024, 10:30 - Sender Name: Message content
var androidFormat = regexp.MustCompile(`^(\d{1,2}/\d{1,2}/\d{4}, \d{1,2}:\d{2}(?: [AP]M)?) - ([^:]+): (.+)$`)

// System message: 01/01/2024, 10:30 - Messages and calls are end-to-end encrypted...
var systemPattern = regexp.MustCompile(`(Messages and calls are end-to-end encrypted|created group|added|removed|left|changed|security code|This message was deleted)`)

// System message with date prefix (no colon-separated sender)
var dateSystemPattern = regexp.MustCompile(`^(\d{1,2}/\d{1,2}/\d{4}, \d{1,2}:\d{2}(?: [AP]M)?) - (.+)$`)

func parseWhatsAppChat(content string) []WhatsAppMessage {
	lines := strings.Split(content, "\n")
	var messages []WhatsAppMessage
	var current *WhatsAppMessage

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Try iOS format
		if matches := iosFormat.FindStringSubmatch(line); matches != nil {
			if current != nil {
				messages = append(messages, *current)
			}
			ts := parseWhatsAppTimestamp(matches[1])
			current = &WhatsAppMessage{
				Timestamp: ts,
				Sender:    strings.TrimSpace(matches[2]),
				Content:   matches[3],
			}
			classifyMessage(current)
			continue
		}

		// Try Android format
		if matches := androidFormat.FindStringSubmatch(line); matches != nil {
			if current != nil {
				messages = append(messages, *current)
			}
			ts := parseWhatsAppTimestamp(matches[1])
			current = &WhatsAppMessage{
				Timestamp: ts,
				Sender:    strings.TrimSpace(matches[2]),
				Content:   matches[3],
			}
			classifyMessage(current)
			continue
		}

		// System message (date followed by system text with no colon-separated sender)
		if dateOnly := dateSystemPattern.FindStringSubmatch(line); dateOnly != nil {
			if current != nil {
				messages = append(messages, *current)
			}
			content := dateOnly[2]
			current = &WhatsAppMessage{
				Timestamp: parseWhatsAppTimestamp(dateOnly[1]),
				Sender:    "System",
				Content:   content,
				IsSystem:  true,
			}
			continue
		}

		// Continuation of previous message (multi-line)
		if current != nil {
			current.Content += "\n" + line
		}
	}

	if current != nil {
		messages = append(messages, *current)
	}

	return messages
}

func classifyMessage(msg *WhatsAppMessage) {
	// Detect system messages
	if systemPattern.MatchString(msg.Content) {
		msg.IsSystem = true
	}

	// Detect media
	mediaMap := map[string]string{
		"<Media omitted>":  "image",
		"image omitted":    "image",
		"video omitted":    "video",
		"audio omitted":    "audio",
		"sticker omitted":  "sticker",
		"GIF omitted":      "image",
		"document omitted": "document",
	}
	for pattern, mtype := range mediaMap {
		if strings.Contains(strings.ToLower(msg.Content), strings.ToLower(pattern)) {
			msg.HasMedia = true
			msg.MediaType = mtype
			break
		}
	}
	if strings.Contains(strings.ToLower(msg.Content), "location: ") {
		msg.HasMedia = true
		msg.MediaType = "location"
	}

	// Detect forwarded
	if strings.Contains(msg.Content, "Forwarded") || strings.Contains(msg.Content, "forwarded") {
		msg.IsForwarded = true
	}
}

func parseWhatsAppTimestamp(s string) time.Time {
	s = strings.TrimSpace(s)
	formats := []string{
		"1/2/2006, 15:04:05",   // iOS 24h
		"1/2/2006, 3:04:05 PM", // iOS 12h
		"1/2/2006, 15:04",      // Android 24h
		"1/2/2006, 3:04 PM",    // Android 12h
		"01/02/2006, 15:04:05",
		"01/02/2006, 3:04:05 PM",
		"01/02/2006, 15:04",
		"01/02/2006, 3:04 PM",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ── Filters ───────────────────────────────────────────────────────────

func filterMessages(msgs []WhatsAppMessage, participant, dateFrom, dateTo string) []WhatsAppMessage {
	var filtered []WhatsAppMessage
	var fromTime, toTime time.Time
	if dateFrom != "" {
		fromTime, _ = time.Parse("2006-01-02", dateFrom)
	}
	if dateTo != "" {
		toTime, _ = time.Parse("2006-01-02", dateTo)
		toTime = toTime.Add(24 * time.Hour) // inclusive
	}

	for _, m := range msgs {
		if participant != "" && !strings.EqualFold(m.Sender, participant) {
			continue
		}
		if !fromTime.IsZero() && m.Timestamp.Before(fromTime) {
			continue
		}
		if !toTime.IsZero() && !m.Timestamp.Before(toTime) {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

// ── Actions ───────────────────────────────────────────────────────────

func (t *WhatsAppData) parseAction(msgs []WhatsAppMessage, limit int) Result {
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("WhatsApp Chat: %d messages\n\n", len(msgs)))
	for _, m := range msgs {
		prefix := ""
		if m.IsSystem {
			prefix = "[SYSTEM] "
		} else if m.HasMedia {
			prefix = fmt.Sprintf("[%s] ", m.MediaType)
		}
		ts := m.Timestamp.Format("2006-01-02 15:04")
		b.WriteString(fmt.Sprintf("%s %s%s: %s\n", ts, prefix, m.Sender, m.Content))
	}

	return Result{Success: true, Output: b.String()}
}

func (t *WhatsAppData) statsAction(msgs []WhatsAppMessage, participant, dateFrom, dateTo string) Result {
	type participantStats struct {
		Name         string
		Messages     int
		MediaCount   int
		SystemCount  int
		FirstMessage time.Time
		LastMessage  time.Time
	}
	stats := make(map[string]*participantStats)
	var totalMedia, totalSystem int
	var firstMsg, lastMsg time.Time

	for _, m := range msgs {
		key := strings.ToLower(m.Sender)
		if _, ok := stats[key]; !ok {
			stats[key] = &participantStats{Name: m.Sender}
		}
		s := stats[key]
		s.Messages++
		if m.HasMedia {
			s.MediaCount++
			totalMedia++
		}
		if m.IsSystem {
			s.SystemCount++
			totalSystem++
		}
		if firstMsg.IsZero() || m.Timestamp.Before(firstMsg) {
			firstMsg = m.Timestamp
		}
		if m.Timestamp.After(lastMsg) {
			lastMsg = m.Timestamp
		}
		if s.FirstMessage.IsZero() || m.Timestamp.Before(s.FirstMessage) {
			s.FirstMessage = m.Timestamp
		}
		if m.Timestamp.After(s.LastMessage) {
			s.LastMessage = m.Timestamp
		}
	}

	var b strings.Builder
	b.WriteString("WhatsApp Chat Statistics\n")
	b.WriteString("========================\n\n")
	b.WriteString(fmt.Sprintf("Total messages: %d\n", len(msgs)))
	b.WriteString(fmt.Sprintf("Participants: %d\n", len(stats)))
	b.WriteString(fmt.Sprintf("Media messages: %d\n", totalMedia))
	b.WriteString(fmt.Sprintf("System messages: %d\n", totalSystem))
	if !firstMsg.IsZero() {
		b.WriteString(fmt.Sprintf("Date range: %s → %s\n", firstMsg.Format("2006-01-02"), lastMsg.Format("2006-01-02")))
		b.WriteString(fmt.Sprintf("Duration: %d days\n", int(lastMsg.Sub(firstMsg).Hours()/24)+1))
	}
	if participant != "" {
		b.WriteString(fmt.Sprintf("Filter: %s\n", participant))
	}
	if dateFrom != "" {
		b.WriteString(fmt.Sprintf("From: %s\n", dateFrom))
	}
	if dateTo != "" {
		b.WriteString(fmt.Sprintf("To: %s\n", dateTo))
	}
	b.WriteString("\nPer Participant:\n")

	// Sort by message count descending
	type kv struct {
		k string
		v *participantStats
	}
	var sorted []kv
	for k, v := range stats {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].v.Messages > sorted[j].v.Messages
	})

	for _, item := range sorted {
		s := item.v
		pct := float64(s.Messages) / float64(len(msgs)) * 100
		b.WriteString(fmt.Sprintf("  %-25s %5d msgs (%5.1f%%)  media: %3d\n", s.Name, s.Messages, pct, s.MediaCount))
	}

	return Result{Success: true, Output: b.String()}
}

func (t *WhatsAppData) searchAction(msgs []WhatsAppMessage, query string, limit int) Result {
	if query == "" {
		return Result{Error: "query is required for search action"}
	}

	queryLower := strings.ToLower(query)
	var results []WhatsAppMessage
	for _, m := range msgs {
		if strings.Contains(strings.ToLower(m.Content), queryLower) {
			results = append(results, m)
		}
	}

	if len(results) > limit {
		results = results[len(results)-limit:]
	}

	if len(results) == 0 {
		return Result{Success: true, Output: fmt.Sprintf("No messages found matching: %s", query)}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Search: %q — %d matches\n\n", query, len(results)))
	for _, m := range results {
		ts := m.Timestamp.Format("2006-01-02 15:04")
		b.WriteString(fmt.Sprintf("[%s] %s: %s\n", ts, m.Sender, m.Content))
	}

	return Result{Success: true, Output: b.String()}
}

func (t *WhatsAppData) participantsAction(msgs []WhatsAppMessage) Result {
	counts := make(map[string]int)         // lowercase key -> count
	displayName := make(map[string]string) // lowercase key -> first-seen display name
	var lowerNames []string                // ordered list of lowercase keys
	seen := make(map[string]bool)
	for _, m := range msgs {
		if m.IsSystem {
			continue
		}
		key := strings.ToLower(m.Sender)
		counts[key]++
		if !seen[key] {
			displayName[key] = m.Sender
			lowerNames = append(lowerNames, key)
			seen[key] = true
		}
	}

	// Sort by display name (case-insensitive, so sort by lowercase key then use display name)
	sort.Slice(lowerNames, func(i, j int) bool {
		return displayName[lowerNames[i]] < displayName[lowerNames[j]]
	})

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Participants: %d\n\n", len(lowerNames)))
	for _, key := range lowerNames {
		count := counts[key]
		b.WriteString(fmt.Sprintf("  %-30s %d messages\n", displayName[key], count))
	}

	return Result{Success: true, Output: b.String()}
}

func (t *WhatsAppData) timelineAction(msgs []WhatsAppMessage) Result {
	if len(msgs) == 0 {
		return Result{Success: true, Output: "No messages in range."}
	}

	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].Timestamp.Before(msgs[j].Timestamp)
	})

	// Group by date
	days := make(map[string]int)
	var dates []string
	for _, m := range msgs {
		dateKey := m.Timestamp.Format("2006-01-02")
		if days[dateKey] == 0 {
			dates = append(dates, dateKey)
		}
		days[dateKey]++
	}

	sort.Strings(dates)

	var b strings.Builder
	b.WriteString("Message Timeline\n")
	b.WriteString("================\n")
	b.WriteString(fmt.Sprintf("Date range: %s → %s\n", dates[0], dates[len(dates)-1]))
	b.WriteString(fmt.Sprintf("Active days: %d\n\n", len(dates)))

	// Show as ASCII chart (max bar width: 40 chars)
	maxCount := 0
	for _, c := range days {
		if c > maxCount {
			maxCount = c
		}
	}

	for _, date := range dates {
		count := days[date]
		barLen := int(float64(count) / float64(maxCount) * 40)
		if barLen == 0 && count > 0 {
			barLen = 1
		}
		bar := strings.Repeat("█", barLen)
		b.WriteString(fmt.Sprintf("  %s  %4d  %s\n", date, count, bar))
	}

	return Result{Success: true, Output: b.String()}
}

// Ensure the whatsapp_parse_chat tool can be looked up by name in tests.
var _ = filepath.Join // reference for the workspace field
