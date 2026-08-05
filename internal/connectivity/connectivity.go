package connectivity

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// Status is the result of a connectivity check.
type Status struct {
	Internet  ComponentStatus `json:"internet"`
	DNS       ComponentStatus `json:"dns"`
	Ports     []PortStatus    `json:"ports,omitempty"`
	Timestamp string          `json:"timestamp"`
}

// ComponentStatus represents the health of a single component.
type ComponentStatus struct {
	Status  string `json:"status"` // "ok", "degraded", "error"
	Message string `json:"message,omitempty"`
	Latency string `json:"latency,omitempty"`
}

// PortStatus is the result of checking a specific port.
type PortStatus struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// RunDiagnostics performs a full connectivity check.
func RunDiagnostics(extraPorts []PortCheck) *Status {
	s := &Status{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Internet check
	s.Internet = checkInternet()

	// DNS check
	s.DNS = checkDNS()

	// Port checks
	s.Ports = checkPorts(extraPorts)

	return s
}

// PortCheck defines a port to probe.
type PortCheck struct {
	Host string
	Port int
	Desc string
}

// checkInternet tries to reach a known-stable endpoint.
func checkInternet() ComponentStatus {
	start := time.Now()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://www.google.com/generate_204")
	latency := time.Since(start)

	if err != nil {
		return ComponentStatus{
			Status:  "error",
			Message: fmt.Sprintf("Cannot reach internet: %v", err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return ComponentStatus{
			Status:  "ok",
			Latency: latency.Round(time.Millisecond).String(),
		}
	}
	return ComponentStatus{
		Status:  "degraded",
		Message: fmt.Sprintf("Internet reachable but returned status %d", resp.StatusCode),
		Latency: latency.Round(time.Millisecond).String(),
	}
}

// checkDNS resolves a known hostname.
func checkDNS() ComponentStatus {
	start := time.Now()
	addrs, err := net.LookupHost("dns.google")
	latency := time.Since(start)

	if err != nil {
		return ComponentStatus{
			Status:  "error",
			Message: fmt.Sprintf("DNS resolution failed: %v", err),
		}
	}
	if len(addrs) == 0 {
		return ComponentStatus{
			Status:  "error",
			Message: "DNS resolved but returned no addresses",
		}
	}
	return ComponentStatus{
		Status:  "ok",
		Message: fmt.Sprintf("Resolved to %s", addrs[0]),
		Latency: latency.Round(time.Millisecond).String(),
	}
}

// checkPorts probes each specified port.
func checkPorts(checks []PortCheck) []PortStatus {
	var results []PortStatus
	for _, c := range checks {
		addr := net.JoinHostPort(c.Host, fmt.Sprintf("%d", c.Port))
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		latency := time.Since(start)

		ps := PortStatus{Host: c.Host, Port: c.Port}
		if err != nil {
			ps.Status = "error"
			ps.Message = fmt.Sprintf("%v", err)
		} else {
			conn.Close()
			ps.Status = "ok"
			ps.Message = fmt.Sprintf("reachable (%s)", latency.Round(time.Millisecond))
		}
		results = append(results, ps)
	}
	return results
}

// FormatReport returns a human-readable connectivity report.
func FormatReport(s *Status) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Connectivity Report (%s)\n\n", s.Timestamp))

	writeComponent(&b, "Internet", s.Internet)
	writeComponent(&b, "DNS", s.DNS)

	if len(s.Ports) > 0 {
		b.WriteString("Port checks:\n")
		for _, p := range s.Ports {
			icon := statusIcon(p.Status)
			b.WriteString(fmt.Sprintf("  %s %s:%d — %s\n", icon, p.Host, p.Port, p.Message))
		}
	}

	overall := "ok"
	if s.Internet.Status == "error" || s.DNS.Status == "error" {
		overall = "error"
	} else if s.Internet.Status == "degraded" {
		overall = "degraded"
	}
	b.WriteString(fmt.Sprintf("\nOverall: %s\n", overall))
	return b.String()
}

func writeComponent(b *strings.Builder, name string, cs ComponentStatus) {
	icon := statusIcon(cs.Status)
	b.WriteString(fmt.Sprintf("%s %s: %s", icon, name, cs.Status))
	if cs.Message != "" {
		b.WriteString(fmt.Sprintf(" — %s", cs.Message))
	}
	if cs.Latency != "" {
		b.WriteString(fmt.Sprintf(" (%s)", cs.Latency))
	}
	b.WriteString("\n")
}

func statusIcon(status string) string {
	switch status {
	case "ok":
		return "[OK]"
	case "degraded":
		return "[WARN]"
	default:
		return "[FAIL]"
	}
}

// QuickCheck runs a fast internet-only check and returns true if reachable.
func QuickCheck() bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://www.google.com/generate_204")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}
