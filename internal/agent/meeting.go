package agent

import "time"

// MeetingSession tracks a single meeting's lifecycle.
type MeetingSession struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	MeetingURL   string     `json:"meeting_url"`
	Status       string     `json:"status"` // joining, active, left, failed
	JoinedAt     *time.Time `json:"joined_at,omitempty"`
	LeftAt       *time.Time `json:"left_at,omitempty"`
	DurationSecs int        `json:"duration_secs"`
	Transcript   string     `json:"transcript,omitempty"`
	MascotID     string     `json:"mascot_id,omitempty"`
	WakePhrase   string     `json:"wake_phrase,omitempty"`
	ListenOnly   bool       `json:"listen_only"`
}

// MeetingSummary is the AI-generated summary of a meeting.
type MeetingSummary struct {
	SessionID    string    `json:"session_id"`
	Title        string    `json:"title"`
	Summary      string    `json:"summary"`
	ActionItems  []string  `json:"action_items"`
	Decisions    []string  `json:"decisions"`
	Participants []string  `json:"participants"`
	CreatedAt    time.Time `json:"created_at"`
}

// MeetingActionItem is a single action item from a meeting.
type MeetingActionItem struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"session_id"`
	Description string     `json:"description"`
	Assignee    string     `json:"assignee,omitempty"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	Completed   bool       `json:"completed"`
	CreatedAt   time.Time  `json:"created_at"`
}

// MeetingStore is the interface for meeting persistence.
type MeetingStore interface {
	SaveSession(session *MeetingSession) error
	GetSession(id string) (*MeetingSession, error)
	ListRecent(limit int) ([]*MeetingSession, error)
	SaveSummary(summary *MeetingSummary) error
	GetSummary(sessionID string) (*MeetingSummary, error)
	SaveActionItem(item *MeetingActionItem) error
	ListActionItems(sessionID string) ([]*MeetingActionItem, error)
}
