package keyring

// RPC provides Wails-bound keyring methods.
type KeyringRPC struct{}

// NewRPC creates a keyring RPC handler.
func NewKeyringRPC() *KeyringRPC {
	return &KeyringRPC{}
}

// KeyringStatus returns the current keyring backend status.
func (r *KeyringRPC) KeyringStatus() map[string]interface{} {
	s := CurrentStatus()
	return map[string]interface{}{
		"available":     s.Available,
		"activeMode":    string(s.ActiveMode),
		"backendName":   s.BackendName,
		"failureReason": failureReasonDisplay(s.FailureReason),
	}
}

func failureReasonDisplay(r *KeyringFailureReason) string {
	if r == nil {
		return ""
	}
	return r.Display()
}

// KeyringConsentDecide records the user's consent decision and returns
// the updated status.
func (r *KeyringRPC) KeyringConsentDecide(mode string) map[string]interface{} {
	if mode == "local_encrypted" {
		RecordConsent("local_encrypted")
	} else {
		RecordConsent("declined")
	}
	return r.KeyringStatus()
}

// KeyringRetryProbe resets the availability cache, re-probes the OS
// keyring, and returns the updated status.
func (r *KeyringRPC) KeyringRetryProbe() map[string]interface{} {
	s := RetryProbe()
	return map[string]interface{}{
		"available":     s.Available,
		"activeMode":    string(s.ActiveMode),
		"backendName":   s.BackendName,
		"failureReason": failureReasonDisplay(s.FailureReason),
	}
}
