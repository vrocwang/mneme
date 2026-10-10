// Shared durations for UI feedback, polling, and RPC. Named constants keep the
// same millisecond value from being retyped (and drifting) across components.

/** How long a transient "saved" / flash message stays on screen. */
export const FLASH_TIMEOUT_MS = 3000;

/** How long a transient success indicator ("Copied!", "Saved") stays up. */
export const FEEDBACK_RESET_MS = 2000;

/** Auto-dismiss delay for toasts. */
export const TOAST_TIMEOUT_MS = 5000;

/** Connectivity re-check cadence. */
export const CONNECTIVITY_POLL_MS = 30_000;

/** Fallback approval poll cadence — pushed events are the primary path. */
export const APPROVAL_POLL_MS = 10_000;

/** Default timeout for a single backend RPC. */
export const RPC_TIMEOUT_MS = 30_000;

/** Timeout for a streaming chat turn, which can run for minutes. */
export const STREAM_TIMEOUT_MS = 120_000;
