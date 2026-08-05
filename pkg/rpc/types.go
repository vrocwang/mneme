// Package rpc provides the JSON-RPC controller framework with schema introspection,
// modeled after the Rust core's ControllerSchema / RpcOutcome<T> pattern.
package rpc

import (
	"encoding/json"
	"fmt"
)

// ── RpcOutcome ────────────────────────────────────────────────────────────────

// RpcOutcome is the standard result envelope for all RPC handlers.
// T is the success payload type. Errors are transported in the Error field.
type RpcOutcome struct {
	Ok    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *RpcError       `json:"error,omitempty"`
}

// RpcError carries a structured error from an RPC handler.
type RpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

func (e *RpcError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("[%s] %s: %s", e.Code, e.Message, e.Detail)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Common error codes.
const (
	ErrCodeInternal           = "internal_error"
	ErrCodeInvalidArgs        = "invalid_args"
	ErrCodeNotFound           = "not_found"
	ErrCodeUnauthorized       = "unauthorized"
	ErrCodeTimeout            = "timeout"
	ErrCodeConflict           = "conflict"
	ErrCodeRateLimited        = "rate_limited"
	ErrCodeForbidden          = "forbidden"
	ErrCodeValidationFailed   = "validation_failed"
	ErrCodePreconditionFailed = "precondition_failed"
	ErrCodeUnavailable        = "unavailable"
	ErrCodeNotImplemented     = "not_implemented"
	ErrCodeQuotaExceeded      = "quota_exceeded"
	ErrCodeCancelled          = "cancelled"
)

// Structured error types that carry typed payloads alongside the error code.
// These mirror the Rust core's StructuredRpcError pattern.

// ValidationError carries field-level validation failures.
type ValidationError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func NewValidationError(fields map[string]string) *ValidationError {
	return &ValidationError{
		Code:    ErrCodeValidationFailed,
		Message: "validation failed",
		Fields:  fields,
	}
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("[%s] %s (%d fields)", e.Code, e.Message, len(e.Fields))
}

// PreconditionError indicates a required precondition was not met.
type PreconditionError struct {
	Code                string   `json:"code"`
	Message             string   `json:"message"`
	FailedPreconditions []string `json:"failed_preconditions"`
}

func NewPreconditionError(reason string, preconditions ...string) *PreconditionError {
	return &PreconditionError{
		Code:                ErrCodePreconditionFailed,
		Message:             reason,
		FailedPreconditions: preconditions,
	}
}

func (e *PreconditionError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// QuotaError indicates a rate or usage limit was exceeded.
type QuotaError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int64  `json:"retry_after_secs,omitempty"`
}

func NewQuotaError(resource string, retryAfterSecs int64) *QuotaError {
	return &QuotaError{
		Code:       ErrCodeQuotaExceeded,
		Message:    fmt.Sprintf("quota exceeded for %s", resource),
		RetryAfter: retryAfterSecs,
	}
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("[%s] %s (retry in %ds)", e.Code, e.Message, e.RetryAfter)
}

// NewForbiddenError creates a standard forbidden outcome.
func NewForbiddenError(reason string) RpcOutcome {
	return NewErrorOutcome(ErrCodeForbidden, "access denied", reason)
}

func NewNotFoundError(resource, id string) RpcOutcome {
	return NewErrorOutcome(ErrCodeNotFound, fmt.Sprintf("%s not found", resource), id)
}

func NewInvalidArgsError(detail string) RpcOutcome {
	return NewErrorOutcome(ErrCodeInvalidArgs, "invalid arguments", detail)
}

func NewTimeoutError(operation string) RpcOutcome {
	return NewErrorOutcome(ErrCodeTimeout, "operation timed out", operation)
}

func NewUnavailableError(service string) RpcOutcome {
	return NewErrorOutcome(ErrCodeUnavailable, "service unavailable", service)
}

func NewNotImplementedError(feature string) RpcOutcome {
	return NewErrorOutcome(ErrCodeNotImplemented, "not implemented", feature)
}

// IsNotFound checks if an RpcOutcome represents a not-found error.
func IsNotFound(out RpcOutcome) bool {
	return out.Error != nil && out.Error.Code == ErrCodeNotFound
}

// IsForbidden checks if an RpcOutcome represents a forbidden error.
func IsForbidden(out RpcOutcome) bool {
	return out.Error != nil && out.Error.Code == ErrCodeForbidden
}

// Outcome helpers.

// NewOutcome wraps a value into a successful RpcOutcome.
func NewOutcome(v interface{}) RpcOutcome {
	data, err := json.Marshal(v)
	if err != nil {
		return NewErrorOutcome(ErrCodeInternal, "marshal failed", err.Error())
	}
	return RpcOutcome{Ok: true, Data: data}
}

// NewErrorOutcome creates a failed RpcOutcome.
func NewErrorOutcome(code, message, detail string) RpcOutcome {
	return RpcOutcome{
		Ok:    false,
		Error: &RpcError{Code: code, Message: message, Detail: detail},
	}
}

// ParseOutcome unmarshals the data field of a successful outcome into dest.
func ParseOutcome(out RpcOutcome, dest interface{}) error {
	if !out.Ok {
		if out.Error != nil {
			return out.Error
		}
		return fmt.Errorf("rpc call failed with no error detail")
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(out.Data, dest)
}

// ── Controller schema types ───────────────────────────────────────────────────

// TypeKind enumerates the JSON-schema type categories used in field schemas.
type TypeKind string

const (
	TypeString TypeKind = "string"
	TypeNumber TypeKind = "number"
	TypeBool   TypeKind = "boolean"
	TypeObject TypeKind = "object"
	TypeArray  TypeKind = "array"
	TypeAny    TypeKind = "any"
)

// FieldSchema describes a single field in a controller's input or output.
type FieldSchema struct {
	Name        string   `json:"name"`
	Type        TypeKind `json:"type"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required"`
	Default     string   `json:"default,omitempty"`
}

// ControllerSchema describes an RPC method: its name, description, inputs, and outputs.
type ControllerSchema struct {
	Namespace   string        `json:"namespace"`
	Method      string        `json:"method"`
	Description string        `json:"description,omitempty"`
	Input       []FieldSchema `json:"input,omitempty"`
	Output      []FieldSchema `json:"output,omitempty"`
}

// FullMethod returns the dot-separated fully-qualified method name (e.g. "agent.chat").
func (c ControllerSchema) FullMethod() string {
	if c.Namespace == "" {
		return c.Method
	}
	return c.Namespace + "." + c.Method
}

// ── Handler types ─────────────────────────────────────────────────────────────

// HandlerFunc is the signature every controller handler must implement.
// It receives raw JSON arguments and returns an RpcOutcome.
type HandlerFunc func(args json.RawMessage) RpcOutcome

// RegisteredController pairs a schema with its handler.
type RegisteredController struct {
	Schema  ControllerSchema
	Handler HandlerFunc
}

// ── Registry ──────────────────────────────────────────────────────────────────

// ControllerRegistry is the global registry of all RPC controllers.
// It mirrors the Rust core's ControllerRegistry pattern: schemas for introspection,
// handlers for dispatch.
type ControllerRegistry struct {
	controllers map[string]RegisteredController // keyed by FullMethod()
}

// NewControllerRegistry creates an empty registry.
func NewControllerRegistry() *ControllerRegistry {
	return &ControllerRegistry{
		controllers: make(map[string]RegisteredController),
	}
}

// Register adds a controller to the registry.
func (r *ControllerRegistry) Register(reg RegisteredController) {
	key := reg.Schema.FullMethod()
	r.controllers[key] = reg
}

// Get looks up a controller by fully-qualified method name.
func (r *ControllerRegistry) Get(method string) (RegisteredController, bool) {
	reg, ok := r.controllers[method]
	return reg, ok
}

// ListSchemas returns all registered controller schemas (for introspection / frontend binding).
func (r *ControllerRegistry) ListSchemas() []ControllerSchema {
	out := make([]ControllerSchema, 0, len(r.controllers))
	for _, reg := range r.controllers {
		out = append(out, reg.Schema)
	}
	return out
}

// Dispatch finds and invokes a handler by method name.
func (r *ControllerRegistry) Dispatch(method string, args json.RawMessage) RpcOutcome {
	reg, ok := r.Get(method)
	if !ok {
		return NewErrorOutcome(ErrCodeNotFound, "method not found", method)
	}
	return reg.Handler(args)
}

// Merge imports all controllers from another registry into this one.
// ── JSON-RPC 2.0 types ──────────────────────────────────────────────────────

// JsonRpcRequest is a standard JSON-RPC 2.0 request.
type JsonRpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id"`
}

// JsonRpcResponse is a standard JSON-RPC 2.0 success response.
type JsonRpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result"`
	ID      interface{} `json:"id"`
}

// JsonRpcError is a standard JSON-RPC 2.0 error response.
type JsonRpcError struct {
	JSONRPC string      `json:"jsonrpc"`
	Error   JsonRpcErr  `json:"error"`
	ID      interface{} `json:"id"`
}

// JsonRpcErr carries the error code and message.
type JsonRpcErr struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard JSON-RPC 2.0 error codes.
const (
	JSONRPCParseError     = -32700
	JSONRPCInvalidRequest = -32600
	JSONRPCMethodNotFound = -32601
	JSONRPCInvalidParams  = -32602
	JSONRPCInternalError  = -32603
)

// ParseJSONRPC parses a JSON-RPC 2.0 request from raw bytes.
func ParseJSONRPC(data []byte) (*JsonRpcRequest, error) {
	var req JsonRpcRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("json-rpc parse: %w", err)
	}
	if req.JSONRPC != "2.0" {
		return nil, fmt.Errorf("json-rpc: invalid or missing jsonrpc version")
	}
	if req.Method == "" {
		return nil, fmt.Errorf("json-rpc: missing method")
	}
	return &req, nil
}

// NewJSONRPCSuccess creates a JSON-RPC 2.0 success response.
func NewJSONRPCSuccess(id interface{}, result interface{}) JsonRpcResponse {
	return JsonRpcResponse{JSONRPC: "2.0", Result: result, ID: id}
}

// NewJSONRPCError creates a JSON-RPC 2.0 error response.
func NewJSONRPCError(id interface{}, code int, message string) JsonRpcError {
	return JsonRpcError{JSONRPC: "2.0", Error: JsonRpcErr{Code: code, Message: message}, ID: id}
}

// ToJSONRPC converts an RpcOutcome to a JSON-RPC 2.0 result or error.
// id can be nil for notifications.
func (out RpcOutcome) ToJSONRPC(id interface{}) interface{} {
	if out.Ok {
		var result interface{}
		json.Unmarshal(out.Data, &result)
		return JsonRpcResponse{JSONRPC: "2.0", Result: result, ID: id}
	}
	code := JSONRPCInternalError
	msg := "internal error"
	if out.Error != nil {
		msg = out.Error.Message
		switch out.Error.Code {
		case ErrCodeNotFound:
			code = JSONRPCMethodNotFound
		case ErrCodeInvalidArgs:
			code = JSONRPCInvalidParams
		case ErrCodeInternal:
			code = JSONRPCInternalError
		}
	}
	return JsonRpcError{JSONRPC: "2.0", Error: JsonRpcErr{Code: code, Message: msg}, ID: id}
}

func (r *ControllerRegistry) Merge(other *ControllerRegistry) {
	for key, reg := range other.controllers {
		r.controllers[key] = reg
	}
}
