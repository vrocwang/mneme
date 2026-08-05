package keyring

import "github.com/simon/mneme/pkg/rpc"

func AllControllerSchemas() []rpc.ControllerSchema {
	return []rpc.ControllerSchema{
		{Namespace: "keyring", Method: "status", Description: "Get current keyring status (available, active mode, backend)"},
		{Namespace: "keyring", Method: "consent_decide", Description: "Record user consent decision for local encrypted storage fallback",
			Input: []rpc.FieldSchema{{Name: "mode", Type: rpc.TypeString, Description: "local_encrypted or declined", Required: true}}},
		{Namespace: "keyring", Method: "retry_probe", Description: "Reset availability cache and re-probe the OS keyring"},
	}
}
