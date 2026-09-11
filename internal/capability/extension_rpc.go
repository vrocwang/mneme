package capability

// WailsRPCRegistrar is implemented by core optional features that contribute a
// Wails-bound RPC type. Registration must happen before wails.Run assembles its
// Bind list (see boot.RegisterWailsRPCs), so feature packages expose a small
// Register function that the boot layer calls at that point.
type WailsRPCRegistrar interface {
	// RegisterRPC returns types to include in the Wails Bind list. Each
	// returned value must be a pointer to a struct with Wails-bound methods.
	RegisterRPC() []interface{}
}

// wailsRPCRegistrars collects types implementing WailsRPCRegistrar. Populated
// before wails.Run; read by main.go to build the Bind list.
var wailsRPCRegistrars []WailsRPCRegistrar

// RegisterWailsRPC adds a WailsRPCRegistrar. Must be called before the Bind
// list is assembled. Not safe for concurrent use — it runs during startup only.
func RegisterWailsRPC(r WailsRPCRegistrar) {
	if r == nil {
		return
	}
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
