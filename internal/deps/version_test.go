package deps

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/schema"
)

// This test intentionally checks compile-time API contracts only. Exact module
// origins are verified by the repository's release commands, not by a networked test.
func TestUpgradeAPIContracts(t *testing.T) {
	var _ = events.WithParentMessageID
	var _ = events.WithUsage
	var _ = events.NewSubagentStartedEvent
	var _ = types.AgentCapabilities{}
	var _ = schema.TokenUsage{}
	var _ = schema.ToolInfo{Extra: map[string]any{}}
}
