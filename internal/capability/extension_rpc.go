package capability

// WailsRPCRegistrar is implemented by extensions that need Wails RPC bindings.
// Extensions register their RPC types at startup so main.go does not need to
// import integration-specific packages.
type WailsRPCRegistrar interface {
	// RegisterRPC returns types to include in the Wails Bind list. Called
	// once at startup before wails.Run. Each returned value must be a
	// pointer to a struct with Wails-bound methods.
	RegisterRPC() []interface{}
}

// RegistryAware is optionally implemented by WailsRPCRegistrar types that
// need a reference to the CapabilityRegistry after it is created during
// app startup.
type RegistryAware interface {
	SetRegistry(reg *CapabilityRegistry)
}

// wailsRPCRegistrars collects types from extensions that implement
// WailsRPCRegistrar. Populated at startup; read by main.go to build
// the wails.Run Bind list without hardcoded integration imports.
var wailsRPCRegistrars []WailsRPCRegistrar

// RegisterWailsRPC adds a WailsRPCRegistrar. Call this from extension
// init() or setup functions. Thread-safe (called during startup only).
func RegisterWailsRPC(r WailsRPCRegistrar) {
	wailsRPCRegistrars = append(wailsRPCRegistrars, r)
}

// CollectWailsRPCBindings returns the aggregated Bind list from all
// registered WailsRPCRegistrar implementations.
func CollectWailsRPCBindings() []interface{} {
	var bindings []interface{}
	for _, r := range wailsRPCRegistrars {
		bindings = append(bindings, r.RegisterRPC()...)
	}
	return bindings
}

// WireRPC sets the CapabilityRegistry on all RegistryAware registrars.
// Called after capReg is created during app startup.
func WireRPC(reg *CapabilityRegistry) {
	for _, r := range wailsRPCRegistrars {
		if ra, ok := r.(RegistryAware); ok {
			ra.SetRegistry(reg)
		}
	}
}
