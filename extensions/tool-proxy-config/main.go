// Proxy Config extension for Mneme.
//
// Provides:
//   - proxy_config: view and set proxy configuration (HTTP/HTTPS)
//
// Protocol plumbing (JSON-RPC over stdio) is provided by pkg/extsdk.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/simon/mneme/pkg/extsdk"
)

func main() {
	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "tool-proxy-config",
		Version:     "0.1.0",
		Description: "View and set HTTP/HTTPS proxy configuration",
	})

	srv.RegisterTool(extsdk.ToolDef{
		Name:        "proxy_config",
		Description: "View or set proxy configuration. Uses http_proxy/https_proxy environment variables and writes to shell config for persistence.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":   map[string]interface{}{"type": "string", "description": "get or set"},
				"protocol": map[string]interface{}{"type": "string", "description": "http or https"},
				"host":     map[string]interface{}{"type": "string", "description": "Proxy host (required for set)"},
				"port":     map[string]interface{}{"type": "number", "description": "Proxy port (required for set)"},
			},
			"required": []string{"action"},
		},
		Permission: "execute",
		HasEffects: true,
	}, proxyConfig)

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tool-proxy-config: %v\n", err)
		os.Exit(1)
	}
}

func proxyConfig(_ context.Context, args map[string]interface{}) extsdk.Result {
	action, _ := args["action"].(string)

	if action == "get" || action == "" {
		// Read proxy from environment and shell config
		result := map[string]interface{}{
			"env": map[string]string{
				"http_proxy":  os.Getenv("http_proxy"),
				"https_proxy": os.Getenv("https_proxy"),
				"HTTP_PROXY":  os.Getenv("HTTP_PROXY"),
				"HTTPS_PROXY": os.Getenv("HTTPS_PROXY"),
				"no_proxy":    os.Getenv("no_proxy"),
				"NO_PROXY":    os.Getenv("NO_PROXY"),
			},
			"shell_config": readShellProxyConfig(),
			"os":           runtime.GOOS,
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return extsdk.Result{Success: true, Output: string(b)}
	}

	if action == "set" {
		protocol, _ := args["protocol"].(string)
		host, _ := args["host"].(string)
		port := 0
		if p, ok := getInt(args, "port"); ok {
			port = p
		}

		if protocol == "" {
			return extsdk.Result{Error: "protocol is required for set action (http or https)"}
		}
		if host == "" || port == 0 {
			return extsdk.Result{Error: "host and port are required for set action"}
		}

		proxyValue := fmt.Sprintf("http://%s:%d", host, port)
		envVar := strings.ToUpper(protocol) + "_PROXY"

		// Set in current process environment
		os.Setenv(envVar, proxyValue)
		os.Setenv(strings.ToLower(protocol)+"_proxy", proxyValue)

		// Write to shell config for persistence
		configFile := shellConfigPath()
		persisted := false
		if configFile != "" {
			if err := writeShellConfig(configFile, envVar, proxyValue); err != nil {
				return extsdk.Result{Error: fmt.Sprintf("write shell config: %v", err)}
			}
			persisted = true
		}

		result := map[string]interface{}{
			"action":    "set",
			"variable":  envVar,
			"value":     proxyValue,
			"persisted": persisted,
		}
		if configFile != "" {
			result["config_file"] = configFile
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return extsdk.Result{Success: true, Output: string(b)}
	}

	return extsdk.Result{Error: fmt.Sprintf("unknown action: %s (use get or set)", action)}
}

func shellConfigPath() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}

	// Detect which shell config file to use
	shell := os.Getenv("SHELL")
	if strings.Contains(shell, "zsh") {
		return filepath.Join(home, ".zshrc")
	}
	if strings.Contains(shell, "bash") {
		// Prefer .bashrc on Linux
		if runtime.GOOS == "linux" {
			return filepath.Join(home, ".bashrc")
		}
		return filepath.Join(home, ".bash_profile")
	}
	// Default
	return filepath.Join(home, ".profile")
}

func readShellProxyConfig() map[string]string {
	configPath := shellConfigPath()
	if configPath == "" {
		return nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}

	lines := strings.Split(string(data), "\n")
	proxy := make(map[string]string)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "export ") {
			parts := strings.SplitN(strings.TrimPrefix(line, "export "), "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				if strings.Contains(strings.ToUpper(key), "PROXY") {
					proxy[key] = strings.Trim(strings.TrimSpace(parts[1]), "\"")
				}
			}
		}
	}
	return proxy
}

func sanitizeShellValue(v string) string {
	// Escape backslash, double-quote, dollar-sign, and backtick for safe shell export.
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "$", "\\$")
	v = strings.ReplaceAll(v, "`", "\\`")
	return v
}

func writeShellConfig(configPath, envVar, value string) error {
	exportLine := fmt.Sprintf("export %s=\"%s\"", envVar, sanitizeShellValue(value))

	data, err := os.ReadFile(configPath)
	if err != nil {
		return os.WriteFile(configPath, []byte(exportLine+"\n"), 0644)
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "export "+envVar+"=") {
			lines[i] = exportLine
			return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
		}
	}

	return os.WriteFile(configPath, []byte(content+"\n"+exportLine+"\n"), 0644)
}

func getInt(args map[string]interface{}, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}
