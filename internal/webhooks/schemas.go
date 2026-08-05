package webhooks

import "github.com/simon/mneme/pkg/rpc"

func AllControllerSchemas() []rpc.ControllerSchema {
	return []rpc.ControllerSchema{
		{Namespace: "webhooks", Method: "list_tunnels", Description: "List all registered webhook tunnels"},
		{Namespace: "webhooks", Method: "create_tunnel", Description: "Create a new webhook tunnel",
			Input: []rpc.FieldSchema{
				{Name: "target", Type: rpc.TypeString, Description: "echo, agent, or skill", Required: true},
				{Name: "target_id", Type: rpc.TypeString, Description: "Agent ID or skill name", Required: true},
				{Name: "description", Type: rpc.TypeString, Description: "Human-readable description"},
			}},
		{Namespace: "webhooks", Method: "get_tunnel", Description: "Get a tunnel by UUID",
			Input: []rpc.FieldSchema{{Name: "uuid", Type: rpc.TypeString, Required: true}}},
		{Namespace: "webhooks", Method: "update_tunnel", Description: "Update a tunnel's enabled state or description",
			Input: []rpc.FieldSchema{
				{Name: "uuid", Type: rpc.TypeString, Required: true},
				{Name: "enabled", Type: rpc.TypeBool},
				{Name: "description", Type: rpc.TypeString},
			}},
		{Namespace: "webhooks", Method: "delete_tunnel", Description: "Delete a tunnel by UUID",
			Input: []rpc.FieldSchema{{Name: "uuid", Type: rpc.TypeString, Required: true}}},
		{Namespace: "webhooks", Method: "get_bandwidth", Description: "Get total bytes received through a tunnel",
			Input: []rpc.FieldSchema{{Name: "uuid", Type: rpc.TypeString, Required: true}}},
		{Namespace: "webhooks", Method: "list_activity", Description: "List recent webhook activity log entries",
			Input: []rpc.FieldSchema{{Name: "limit", Type: rpc.TypeNumber, Description: "Max entries (default 50)"}}},
		{Namespace: "webhooks", Method: "clear_activity", Description: "Clear the webhook activity log"},
	}
}
