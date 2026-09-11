package notifications

// RPC exposes the notification bus to the Wails frontend so notifications that
// are persisted by the bus can also be listed, read, and counted. The bus is
// created during startup (after the Wails Bind list is evaluated), so it is
// resolved through a getter rather than captured at construction time.
type RPC struct {
	bus func() *Bus
}

// NewRPC creates a notification RPC handler. busFn is evaluated per call.
func NewRPC(busFn func() *Bus) *RPC {
	return &RPC{bus: busFn}
}

func (r *RPC) resolve() *Bus {
	if r == nil || r.bus == nil {
		return nil
	}
	return r.bus()
}

// ListNotifications returns the most recent notifications, newest first.
// A non-positive limit falls back to the bus default.
func (r *RPC) ListNotifications(limit int) ([]Notification, error) {
	b := r.resolve()
	if b == nil {
		return nil, nil
	}
	return b.ListRecent(limit)
}

// ListUnreadNotifications returns unread notifications, newest first.
func (r *RPC) ListUnreadNotifications(limit int) ([]Notification, error) {
	b := r.resolve()
	if b == nil {
		return nil, nil
	}
	return b.ListUnread(limit)
}

// MarkNotificationRead marks a single notification as read.
func (r *RPC) MarkNotificationRead(id string) error {
	b := r.resolve()
	if b == nil {
		return nil
	}
	return b.MarkRead(id)
}

// MarkAllNotificationsRead marks every unread notification as read.
func (r *RPC) MarkAllNotificationsRead() error {
	b := r.resolve()
	if b == nil {
		return nil
	}
	return b.MarkAllRead()
}

// UnreadNotificationCount returns the number of unread notifications, so the
// UI can render a badge without fetching the full list.
func (r *RPC) UnreadNotificationCount() (int, error) {
	b := r.resolve()
	if b == nil {
		return 0, nil
	}
	return b.UnreadCount()
}
