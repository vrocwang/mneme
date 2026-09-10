package threads

// ThreadSummary describes a conversation thread for list views.
type ThreadSummary struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Labels        []string `json:"labels,omitempty"`
	PersonalityID string   `json:"personalityId,omitempty"`
	MessageCount  int      `json:"message_count"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

// MessageRecord is a single persisted message in a thread.
type MessageRecord struct {
	ID        int64             `json:"id"`
	ThreadID  string            `json:"threadId"`
	Role      string            `json:"role"`
	Content   string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
	CreatedAt string            `json:"created_at"`
}
