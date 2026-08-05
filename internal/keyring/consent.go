// Package keyring provides the consent gate for keyring fallback.
// All code paths that read or write secrets should call CheckSecretAccess
// instead of raw IsAvailable(). This centralises the consent check so the
// app never silently falls back to local encrypted storage without the
// user's explicit agreement.
package keyring

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ── Types ──────────────────────────────────────────────────────────────

// StorageMode represents the active secret storage mode.
type StorageMode string

const (
	ModeOsKeyring      StorageMode = "os_keyring"
	ModeLocalEncrypted StorageMode = "local_encrypted"
	ModeConsentPending StorageMode = "consent_pending"
	ModeDeclined       StorageMode = "declined"
)

// KeyringFailureReason classifies why the OS keyring is unavailable.
type KeyringFailureReason string

const (
	FailureNoSecretService      KeyringFailureReason = "no_secret_service"
	FailureKeychainLocked       KeyringFailureReason = "keychain_locked"
	FailureAccessDenied         KeyringFailureReason = "access_denied"
	FailureMasterKeyUnavailable KeyringFailureReason = "master_key_unavailable"
	FailureUnknown              KeyringFailureReason = "unknown"
)

func (r KeyringFailureReason) Display() string {
	switch r {
	case FailureNoSecretService:
		return "No Secret Service daemon available"
	case FailureKeychainLocked:
		return "OS keychain is locked"
	case FailureAccessDenied:
		return "Access to OS keychain was denied"
	case FailureMasterKeyUnavailable:
		return "Master encryption key unavailable"
	default:
		return string(r)
	}
}

// KeyringStatus is the public-facing status for RPC / UI consumption.
type KeyringStatus struct {
	Available     bool                  `json:"available"`
	FailureReason *KeyringFailureReason `json:"failureReason,omitempty"`
	ActiveMode    StorageMode           `json:"activeMode"`
	BackendName   string                `json:"backendName"`
}

// ConsentPreference captures the user's decision about fallback storage.
type ConsentPreference struct {
	StorageMode   string `json:"storageMode"`
	ConsentedAtMs *int64 `json:"consentedAtMs,omitempty"`
}

// PolicyDecision is the result of CheckSecretAccess.
type PolicyDecision int

const (
	DecisionProceed PolicyDecision = iota
	DecisionConsentRequired
	DecisionDeclined
)

// ConsentEventPublisher is the interface for publishing consent events.
type ConsentEventPublisher interface {
	PublishKeyringConsentRequired()
	PublishKeyringDecryptFailed(fieldName, reason string)
}

// ── Process-global state ───────────────────────────────────────────────

var (
	consentCache          sync.RWMutex // protects consentPref
	consentPref           *ConsentPreference
	consentEventPublished atomic.Bool
	consentEventPub       ConsentEventPublisher
	consentLog            *slog.Logger
)

func init() {
	consentLog = slog.Default()
}

// SetConsentLogger sets the logger for consent events.
func SetConsentLogger(log *slog.Logger) {
	if log != nil {
		consentLog = log
	}
}

// SetConsentEventPublisher sets the event publisher for consent events.
func SetConsentEventPublisher(pub ConsentEventPublisher) {
	consentEventPub = pub
}

// ── Policy ─────────────────────────────────────────────────────────────

// Initialize pre-populates the consent cache from persisted app state.
// Call once at startup after config is loadable.
func Initialize(consent *ConsentPreference) {
	consentLog.Info("[keyring_consent] initialize",
		"cached_consent", consentModeString(consent))
	consentCache.Lock()
	consentPref = consent
	consentCache.Unlock()
}

// CheckSecretAccess returns whether the caller may proceed with secret
// storage. On platforms with a native keyring (macOS Keychain, Linux
// Secret Service), it checks keyring availability and falls back to the
// user's consent preference. On platforms without a native keyring
// (Windows), file storage is the only option and consent is auto-granted.
func CheckSecretAccess() PolicyDecision {
	if IsAvailable() {
		return DecisionProceed
	}

	consentCache.RLock()
	pref := consentPref
	consentCache.RUnlock()

	if pref != nil {
		switch pref.StorageMode {
		case "local_encrypted":
			consentLog.Debug("[keyring_consent] check_secret_access: consent=local_encrypted, proceeding")
			return DecisionProceed
		case "declined":
			consentLog.Debug("[keyring_consent] check_secret_access: consent=declined")
			return DecisionDeclined
		}
	}

	// On platforms without a native keyring, auto-consent to file storage.
	// There's no other option — asking the user is pointless.
	if !isNativeKeyringSupported() {
		consentLog.Info("[keyring_consent] check_secret_access: no native keyring — auto-consenting to file storage")
		return DecisionProceed
	}

	// No consent recorded yet — publish event (deduped per session).
	consentLog.Debug("[keyring_consent] check_secret_access: keyring unavailable, no consent recorded")
	if !consentEventPublished.Swap(true) {
		consentLog.Info("[keyring_consent] publishing KeyringConsentRequired event")
		if consentEventPub != nil {
			consentEventPub.PublishKeyringConsentRequired()
		}
	}
	return DecisionConsentRequired
}

// CurrentStatus builds the current keyring status for RPC / UI.
func CurrentStatus() KeyringStatus {
	available := IsAvailable()
	backendName := BackendName()

	if available {
		return KeyringStatus{
			Available:   true,
			ActiveMode:  ModeOsKeyring,
			BackendName: backendName,
		}
	}

	// On platforms without a native keyring, report local_encrypted directly.
	if !isNativeKeyringSupported() {
		return KeyringStatus{
			Available:   false,
			ActiveMode:  ModeLocalEncrypted,
			BackendName: "file",
		}
	}

	reason := classifyFailureReason(backendName)

	consentCache.RLock()
	pref := consentPref
	consentCache.RUnlock()

	mode := ModeConsentPending
	if pref != nil {
		switch pref.StorageMode {
		case "local_encrypted":
			mode = ModeLocalEncrypted
		case "declined":
			mode = ModeDeclined
		}
	}

	return KeyringStatus{
		Available:     false,
		FailureReason: &reason,
		ActiveMode:    mode,
		BackendName:   backendName,
	}
}

// BuildConsentPreference builds a consent preference value without
// touching the in-memory cache. Callers should persist to disk first,
// then call ApplyConsent. This ordering ensures cache and disk never
// diverge (if persistence fails, cache is not updated).
func BuildConsentPreference(mode string) ConsentPreference {
	nowMs := time.Now().UnixMilli()
	return ConsentPreference{
		StorageMode:   mode,
		ConsentedAtMs: &nowMs,
	}
}

// ApplyConsent applies a previously-built consent preference to the
// in-memory cache. Call only after the preference has been successfully
// persisted to disk.
func ApplyConsent(pref *ConsentPreference) {
	consentLog.Info("[keyring_consent] apply_consent",
		"mode", pref.StorageMode,
		"at_ms", pref.ConsentedAtMs)
	consentCache.Lock()
	consentPref = pref
	consentCache.Unlock()
	consentEventPublished.Store(false)
}

// RecordConsent updates the in-memory cache and returns the preference
// for the caller to persist. Prefer BuildConsentPreference + ApplyConsent
// when you need to guarantee persistence before cache update.
func RecordConsent(mode string) ConsentPreference {
	pref := BuildConsentPreference(mode)
	consentLog.Info("[keyring_consent] record_consent",
		"mode", mode,
		"at_ms", pref.ConsentedAtMs)
	ApplyConsent(&pref)
	return pref
}

// RetryProbe resets the cached keyring availability and re-runs the probe.
func RetryProbe() KeyringStatus {
	consentLog.Info("[keyring_consent] retry_probe: resetting availability cache")
	ResetAvailabilityCache()
	consentEventPublished.Store(false)
	return CurrentStatus()
}

// NotifyMasterKeyUnavailable surfaces a master-key load failure to the
// frontend by publishing the consent-required event. Call proactively at
// startup when the encrypted-file backend cannot load its master key.
func NotifyMasterKeyUnavailable(reason string) {
	consentLog.Warn("[keyring_consent] master key unavailable: " + reason)
	if !consentEventPublished.Swap(true) {
		consentLog.Info("[keyring_consent] publishing KeyringConsentRequired event (master key unavailable)")
		if consentEventPub != nil {
			consentEventPub.PublishKeyringConsentRequired()
		}
	}
}

// NotifyDecryptFailure publishes a decrypt-failure event for frontend
// notification.
func NotifyDecryptFailure(fieldName, reason string) {
	consentLog.Warn("[keyring_consent] decrypt failure",
		"field", fieldName,
		"reason", reason)
	if consentEventPub != nil {
		consentEventPub.PublishKeyringDecryptFailed(fieldName, reason)
	}
}

// ── Backend integration ────────────────────────────────────────────────

// IsAvailable checks whether any OS keyring backend is available.
//
// It prefers the backend's Probe method (which performs a raw reachability
// check) over Get: the macOS/Linux Get implementations route "item not
// found" through the consent fallback, which calls CheckSecretAccess ->
// IsAvailable, so probing via Get would recurse infinitely. Probe avoids
// that cycle and correctly treats a responsive "not found" (exit 44) as
// "keychain reachable".
func IsAvailable() bool {
	s := defaultStore()
	if p, ok := s.(Prober); ok {
		return p.Probe()
	}
	// Backends without a native keyring (FileStore) have no Prober; a
	// Get-based probe is safe for them because their Get never recurses.
	_, err := s.Get("__mneme_probe__", "__probe__")
	return err == nil
}

// BackendName returns the name of the active keyring backend.
func BackendName() string {
	s := defaultStore()
	switch s.(type) {
	case *FileStore:
		// Check if we're using the platform keyring or file fallback.
		// On Linux, the platform keyring wraps secret-tool; on macOS, Keychain.
		return "file"
	default:
		return "os"
	}
}

// ResetAvailabilityCache clears any cached availability state so the
// next probe is fresh. Implementations should clear internal error caches.
func ResetAvailabilityCache() {
	// Force the next IsAvailable() to re-probe by clearing any cached state.
	// The defaultStore function already creates a fresh probe each call,
	// so this is primarily about clearing consent event state (done by caller).
}

// ── Helpers ────────────────────────────────────────────────────────────

func classifyFailureReason(backendName string) KeyringFailureReason {
	// Platform-specific classification to match the Rust implementation.
	switch backendName {
	case "os":
		return FailureNoSecretService
	case "encrypted_file":
		return FailureMasterKeyUnavailable
	default:
		return FailureUnknown
	}
}

func consentModeString(pref *ConsentPreference) string {
	if pref == nil {
		return "none"
	}
	return pref.StorageMode
}
