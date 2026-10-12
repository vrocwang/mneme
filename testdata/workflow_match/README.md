# Shared workflow-match fixture

Both WORKFLOW.md matchers — the core prompt injector
(`internal/agent/workflow_inject.go`) and the workflows extension
(`extensions/workflows/workflow_ops.go`, used by `workflow_match`) — assert
against this directory. The core decides which workflows the model is told
about; the extension decides which one the tool names, so a divergence would
have them contradict each other.

`expected.json` records the task to match and the workflow directory that must
win. Change the fixture only together with both tests.
