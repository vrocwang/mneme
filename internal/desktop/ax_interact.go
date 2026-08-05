package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/simon/mneme/internal/tools"
	"strings"
)

// AXElement describes a UI element found via accessibility APIs.
type AXElement struct {
	Role        string `json:"role"`
	Label       string `json:"label"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
	Position    string `json:"position,omitempty"` // "x,y"
	Size        string `json:"size,omitempty"`     // "w,h"
	Enabled     bool   `json:"enabled"`
	Focused     bool   `json:"focused"`
}

// AXRing is the result of an accessibility interaction.
type AXRing struct {
	Elements []AXElement `json:"elements,omitempty"`
	Text     string      `json:"text,omitempty"`
	Error    string      `json:"error,omitempty"`
}

// AXInteract provides cross-platform accessibility interaction.
type AXInteract struct{}

// NewAXInteract creates an accessibility interaction helper.
func NewAXInteract() *AXInteract { return &AXInteract{} }

// GetFocusedElement returns information about the currently focused element.
func (a *AXInteract) GetFocusedElement() (*AXRing, error) {
	switch runtime.GOOS {
	case "darwin":
		return a.getFocusedDarwin()
	case "linux":
		return a.getFocusedLinux()
	case "windows":
		return a.getFocusedWindows()
	default:
		return nil, fmt.Errorf("ax_interact: unsupported platform")
	}
}

// ListElements returns all interactive controls in the frontmost application,
// optionally filtered by a case-insensitive substring. Caps at 60 results.
// Matches the Rust ax_list_elements_filtered behaviour.
func (a *AXInteract) ListElements(filter string) (*AXRing, error) {
	switch runtime.GOOS {
	case "darwin":
		return a.listElementsDarwin(filter)
	case "windows":
		return a.listElementsWindows(filter)
	default:
		return &AXRing{Error: "ax_interact list: supported on macOS and Windows only"}, nil
	}
}

// FindElement searches for a UI element matching the given criteria.
// query: text to search for in labels/values.
func (a *AXInteract) FindElement(query string) (*AXRing, error) {
	switch runtime.GOOS {
	case "darwin":
		return a.findElementDarwin(query)
	case "linux":
		return a.findElementLinux(query)
	case "windows":
		return a.findElementWindows(query)
	default:
		return nil, fmt.Errorf("ax_interact: unsupported platform")
	}
}

// ClickElement clicks the UI element at the given coordinates.
func (a *AXInteract) ClickElement(x, y int) error {
	cc := tools.NewComputerControl()
	result := cc.Execute(context.Background(), map[string]interface{}{
		"action": "mouse_click", "x": float64(x), "y": float64(y),
	})
	if result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}
	return nil
}

// CountElements returns the total number of interactive UI elements in the
// frontmost application. Used by SettleWait to detect when a UI has finished
// rendering after an action. Returns -1 if the count cannot be determined.
func (a *AXInteract) CountElements() int {
	switch runtime.GOOS {
	case "darwin":
		return a.countElementsDarwin()
	case "windows":
		return a.countElementsWindows()
	default:
		return -1
	}
}

func (a *AXInteract) countElementsDarwin() int {
	script := `tell application "System Events"
	set frontProc to first application process whose frontmost is true
	return count of every UI element of entire contents of front window of frontProc
end tell`
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return -1
	}
	var count int
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &count)
	return count
}

func (a *AXInteract) countElementsWindows() int {
	script := `Add-Type -AssemblyName UIAutomationClient
$root = [System.Windows.Automation.AutomationElement]::FocusedElement
$cond = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::IsControlElementProperty, $true)
$elements = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, $cond)
$elements.Count`
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return -1
	}
	var count int
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &count)
	return count
}

const maxListElements = 60

// listElementsDarwin returns all interactive UI elements in the frontmost app.
func (a *AXInteract) listElementsDarwin(filter string) (*AXRing, error) {
	script := fmt.Sprintf(`tell application "System Events"
	set frontProc to first application process whose frontmost is true
	set allElems to every UI element of entire contents of front window of frontProc
	set output to ""
	set count to 0
	repeat with e in allElems
		if count >= %d then exit repeat
		try
			set elemRole to role of e
			set elemDesc to description of e
		on error
			set elemRole to ""
			set elemDesc to ""
		end try
		set line to elemRole & "|" & elemDesc & "\n"
		output := output & line
		set count to count + 1
	end repeat
	return output
end tell`, maxListElements)
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return &AXRing{Error: fmt.Sprintf("list darwin: %v: %s", err, out)}, nil
	}
	return a.parseListOutput(string(out), filter), nil
}

// listElementsWindows returns interactive controls via UIAutomation.
func (a *AXInteract) listElementsWindows(filter string) (*AXRing, error) {
	script := fmt.Sprintf(`Add-Type -AssemblyName UIAutomationClient
$root = [System.Windows.Automation.AutomationElement]::FocusedElement
$cond = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::IsControlElementProperty, $true)
$elements = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, $cond)
$count = 0
foreach ($e in $elements) {
	if ($count -ge %d) { break }
	$role = $e.Current.ControlType.ProgrammaticName -replace '^ControlType\.', ''
	$label = $e.Current.Name
	"$role|$label"
	$count++
}`, maxListElements)
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return &AXRing{Error: fmt.Sprintf("list windows: %v: %s", err, out)}, nil
	}
	return a.parseListOutput(string(out), filter), nil
}

func (a *AXInteract) parseListOutput(output, filter string) *AXRing {
	var elements []AXElement
	lowerFilter := strings.ToLower(filter)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		role, label := parts[0], ""
		if len(parts) > 1 {
			label = parts[1]
		}
		if filter != "" && !strings.Contains(strings.ToLower(label), lowerFilter) &&
			!strings.Contains(strings.ToLower(role), lowerFilter) {
			continue
		}
		elements = append(elements, AXElement{Role: role, Label: label})
	}
	if len(elements) > maxListElements {
		elements = elements[:maxListElements]
	}
	return &AXRing{Elements: elements}
}

// ── macOS ──────────────────────────────────────────────────────────

func (a *AXInteract) getFocusedDarwin() (*AXRing, error) {
	script := `tell application "System Events"
	set frontProc to first application process whose frontmost is true
	set focusedElem to focused UI element of frontProc
	set elemRole to role of focusedElem
	set elemDesc to description of focusedElem
	try
		set elemValue to value of focusedElem
	on error
		set elemValue to ""
	end try
	try
		set elemPos to position of focusedElem
	on error
		set elemPos to {0, 0}
	end try
	try
		set elemSize to size of focusedElem
	on error
		set elemSize to {0, 0}
	end try
	return elemRole & "|" & elemDesc & "|" & elemValue & "|" & item 1 of elemPos & "," & item 2 of elemPos & "|" & item 1 of elemSize & "," & item 2 of elemSize
end tell`

	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return &AXRing{Error: fmt.Sprintf("AX error: %v: %s", err, out)}, nil
	}

	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 5)
	elem := AXElement{Role: safeIdx(parts, 0), Description: safeIdx(parts, 1),
		Value: safeIdx(parts, 2), Enabled: true, Focused: true}
	if len(parts) > 3 {
		elem.Position = parts[3]
	}
	if len(parts) > 4 {
		elem.Size = parts[4]
	}
	return &AXRing{Elements: []AXElement{elem}}, nil
}

func (a *AXInteract) findElementDarwin(query string) (*AXRing, error) {
	escaped := strings.ReplaceAll(query, `"`, `\"`)
	script := fmt.Sprintf(`tell application "System Events"
	set frontProc to first application process whose frontmost is true
	set results to {}
	repeat with elem in (every UI element of front window of frontProc)
		try
			set elemDesc to description of elem
			if elemDesc contains "%s" then
				set end of results to (role of elem & "|" & elemDesc)
			end if
		end try
		try
			set elemName to name of elem
			if elemName contains "%s" then
				set end of results to (role of elem & "|" & elemName)
			end if
		end try
	end repeat
	return results as string
end tell`, escaped, escaped)

	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return &AXRing{Error: fmt.Sprintf("find element: %v", err)}, nil
	}

	var elements []AXElement
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		e := AXElement{Role: parts[0], Enabled: true}
		if len(parts) > 1 {
			e.Description = parts[1]
		}
		elements = append(elements, e)
	}
	return &AXRing{Elements: elements}, nil
}

// ── Linux (AT-SPI via dbus-send / gdbus) ───────────────────────────

func (a *AXInteract) getFocusedLinux() (*AXRing, error) {
	// Attempt via gdbus (GNOME), fall back to xdotool.
	out, err := exec.Command("gdbus", "call", "--session",
		"--dest", "org.a11y.Bus", "--object-path", "/org/a11y/bus",
		"--method", "org.a11y.Bus.GetAddress").CombinedOutput()
	if err != nil {
		// Fallback: use xdotool for basic info.
		return a.xdotoolFocus()
	}
	_ = out
	return a.xdotoolFocus()
}

func (a *AXInteract) xdotoolFocus() (*AXRing, error) {
	out, err := exec.Command("xdotool", "getactivewindow", "getwindowname").CombinedOutput()
	if err != nil {
		return &AXRing{Error: fmt.Sprintf("xdotool: %v", err)}, nil
	}
	return &AXRing{
		Text:     strings.TrimSpace(string(out)),
		Elements: []AXElement{{Role: "window", Label: strings.TrimSpace(string(out)), Focused: true}},
	}, nil
}

func (a *AXInteract) findElementLinux(query string) (*AXRing, error) {
	// Search window titles via xdotool. Full AT-SPI2 tree search requires
	// at-spi2-core and would enable richer element matching.
	out, err := exec.Command("xdotool", "search", "--name", query, "getwindowname").CombinedOutput()
	if err != nil {
		return &AXRing{
			Error: fmt.Sprintf("xdotool search failed (install xdotool or at-spi2-core): %v", err),
		}, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return &AXRing{Text: "no matching windows found"}, nil
	}
	return &AXRing{
		Text:     strings.Join(lines, ", "),
		Elements: []AXElement{{Role: "window", Label: lines[0], Focused: false}},
	}, nil
}

// ── Windows (UI Automation via PowerShell) ─────────────────────────

func (a *AXInteract) getFocusedWindows() (*AXRing, error) {
	script := `Add-Type -AssemblyName UIAutomationClient
$automation = [System.Windows.Automation.AutomationElement]::FocusedElement
if ($automation -eq $null) { Write-Output "ERROR|no focused element" }
else {
	$role = $automation.Current.ControlType.ProgrammaticName
	$name = $automation.Current.Name
	$class = $automation.Current.ClassName
	$rect = $automation.Current.BoundingRectangle
	Write-Output "$role|$name|$class|$($rect.X),$($rect.Y)|$($rect.Width),$($rect.Height)"
}`

	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		return &AXRing{Error: fmt.Sprintf("UIA: %v: %s", err, out)}, nil
	}

	result := strings.TrimSpace(string(out))
	if strings.HasPrefix(result, "ERROR|") {
		return &AXRing{Error: strings.TrimPrefix(result, "ERROR|")}, nil
	}

	parts := strings.SplitN(result, "|", 5)
	return &AXRing{Elements: []AXElement{{
		Role:     safeIdx(parts, 0),
		Label:    safeIdx(parts, 1),
		Enabled:  true,
		Focused:  true,
		Position: safeIdx(parts, 3),
		Size:     safeIdx(parts, 4),
	}}}, nil
}

func (a *AXInteract) findElementWindows(query string) (*AXRing, error) {
	escaped := strings.ReplaceAll(query, "'", "''")
	script := fmt.Sprintf(`Add-Type -AssemblyName UIAutomationClient
$root = [System.Windows.Automation.AutomationElement]::RootElement
$cond = New-Object System.Windows.Automation.PropertyCondition(
	[System.Windows.Automation.AutomationElement]::NameProperty, '%s')
$results = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, $cond)
foreach ($el in $results) {
	Write-Output "$($el.Current.ControlType.ProgrammaticName)|$($el.Current.Name)"
}`, escaped)

	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		return &AXRing{Error: fmt.Sprintf("UIA find: %v", err)}, nil
	}

	var elements []AXElement
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		e := AXElement{Role: parts[0], Enabled: true}
		if len(parts) > 1 {
			e.Label = parts[1]
		}
		elements = append(elements, e)
	}
	return &AXRing{Elements: elements}, nil
}

func safeIdx(parts []string, i int) string {
	if i < len(parts) {
		return parts[i]
	}
	return ""
}
