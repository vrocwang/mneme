package keyring

import (
	"testing"
)

func TestConsent_BuildAndApply(t *testing.T) {
	// Reset state for test isolation.
	consentCache.Lock()
	consentPref = nil
	consentCache.Unlock()
	consentEventPublished.Store(false)

	pref := BuildConsentPreference("local_encrypted")
	if pref.StorageMode != "local_encrypted" {
		t.Errorf("expected local_encrypted, got %s", pref.StorageMode)
	}
	if pref.ConsentedAtMs == nil {
		t.Error("consented_at_ms should be set")
	}

	ApplyConsent(&pref)

	consentCache.RLock()
	cached := consentPref
	consentCache.RUnlock()

	if cached == nil || cached.StorageMode != "local_encrypted" {
		t.Errorf("cache should have local_encrypted, got %v", cached)
	}
}

func TestConsent_RecordConsent(t *testing.T) {
	consentCache.Lock()
	consentPref = nil
	consentCache.Unlock()
	consentEventPublished.Store(false)

	pref := RecordConsent("declined")
	if pref.StorageMode != "declined" {
		t.Errorf("expected declined, got %s", pref.StorageMode)
	}

	consentCache.RLock()
	cached := consentPref
	consentCache.RUnlock()

	if cached == nil || cached.StorageMode != "declined" {
		t.Errorf("cache should have declined, got %v", cached)
	}

	// CheckSecretAccess should return DecisionDeclined when keyring is
	// not available and consent is declined.
	decision := CheckSecretAccess()
	if decision != DecisionDeclined {
		t.Logf("CheckSecretAccess = %v (may be Proceed if OS keyring is available)", decision)
	}
}

func TestConsent_Initialize(t *testing.T) {
	pref := &ConsentPreference{
		StorageMode:   "local_encrypted",
		ConsentedAtMs: int64Ptr(12345),
	}
	Initialize(pref)

	consentCache.RLock()
	cached := consentPref
	consentCache.RUnlock()

	if cached == nil || cached.StorageMode != "local_encrypted" {
		t.Errorf("cache should be initialized, got %v", cached)
	}
}

func TestConsent_CurrentStatus(t *testing.T) {
	status := CurrentStatus()
	if status.BackendName == "" {
		t.Error("backend name should not be empty")
	}
	// Status should always return a valid struct.
	if status.ActiveMode == "" {
		t.Error("active mode should not be empty")
	}
}

func TestConsent_ClassifyFailureReason(t *testing.T) {
	if r := classifyFailureReason("os"); r != FailureNoSecretService {
		t.Errorf("os backend should classify as no_secret_service, got %s", r)
	}
	if r := classifyFailureReason("encrypted_file"); r != FailureMasterKeyUnavailable {
		t.Errorf("encrypted_file backend should classify as master_key_unavailable, got %s", r)
	}
	if r := classifyFailureReason("weird"); r != FailureUnknown {
		t.Errorf("unknown backend should classify as unknown, got %s", r)
	}
}

func TestConsent_FailureReasonDisplay(t *testing.T) {
	if s := FailureNoSecretService.Display(); s != "No Secret Service daemon available" {
		t.Errorf("unexpected display: %s", s)
	}
	if s := FailureKeychainLocked.Display(); s != "OS keychain is locked" {
		t.Errorf("unexpected display: %s", s)
	}
}

func TestConsent_BuildConsentPreference_InvalidMode(t *testing.T) {
	// BuildConsentPreference accepts any mode string; validation is the
	// caller's responsibility (the RPC layer).
	pref := BuildConsentPreference("invalid")
	if pref.StorageMode != "invalid" {
		t.Errorf("BuildConsentPreference should accept any mode string, got %s", pref.StorageMode)
	}
}

func int64Ptr(v int64) *int64 { return &v }
