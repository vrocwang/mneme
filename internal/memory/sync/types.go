package sync

// PipelineKind categorises a sync pipeline by its upstream type.
type PipelineKind string

const (
	KindComposio  PipelineKind = "composio"
	KindMcp       PipelineKind = "mcp"
	KindWorkspace PipelineKind = "workspace"
)

func (k PipelineKind) String() string { return string(k) }

// SyncReason describes why a sync was triggered.
type SyncReason string

const (
	ReasonConnectionCreated SyncReason = "connection_created"
	ReasonPeriodic          SyncReason = "periodic"
	ReasonManual            SyncReason = "manual"
	ReasonTrigger           SyncReason = "trigger"
)

func (r SyncReason) String() string { return string(r) }
