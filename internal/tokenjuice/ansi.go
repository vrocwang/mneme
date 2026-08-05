package tokenjuice

import "regexp"

var (
	ansiCSI           = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	ansiOSC           = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	ansiIncompleteCSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*$`)
	ansiIncompleteOSC = regexp.MustCompile(`\x1b\][^\x07\x1b]*$`)
	ansiSingle        = regexp.MustCompile(`\x1b[@-_]`)
	ansiRaw           = regexp.MustCompile(`\x1b`)
)

// StripANSI removes ANSI/VT escape sequences from text.
// Handles CSI (colors, cursor), OSC (hyperlinks, window titles),
// incomplete sequences at end-of-string, and single-char escapes.
func StripANSI(text string) string {
	// Order matters: OSC before CSI to avoid matching \x1b\ inside a CSI
	text = ansiOSC.ReplaceAllString(text, "")
	text = ansiIncompleteOSC.ReplaceAllString(text, "")
	text = ansiCSI.ReplaceAllString(text, "")
	text = ansiIncompleteCSI.ReplaceAllString(text, "")
	text = ansiSingle.ReplaceAllString(text, "")
	text = ansiRaw.ReplaceAllString(text, "")
	return text
}
