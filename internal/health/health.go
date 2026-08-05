package health

import "sync"

type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type HealthStatus struct {
	Status string        `json:"status"`
	Checks []CheckResult `json:"checks"`
}

type CheckFn func() CheckResult

var (
	mu     sync.Mutex
	checks = map[string]CheckFn{}
)

func Register(name string, fn CheckFn) {
	mu.Lock()
	defer mu.Unlock()
	checks[name] = fn
}

func Check() HealthStatus {
	mu.Lock()
	fns := make(map[string]CheckFn, len(checks))
	for k, v := range checks {
		fns[k] = v
	}
	mu.Unlock()

	var results []CheckResult
	hasError := false
	for name, fn := range fns {
		r := fn()
		r.Name = name
		results = append(results, r)
		if r.Status != "ok" {
			hasError = true
		}
	}

	status := "ok"
	if hasError {
		status = "degraded"
	}

	return HealthStatus{Status: status, Checks: results}
}
