// Package deps pins module-level dependencies before the implementation
// packages consume them directly.
package deps

import (
	_ "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	_ "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	_ "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	_ "github.com/cloudwego/eino/components/model"
	_ "github.com/cloudwego/eino/components/tool"
	_ "github.com/cloudwego/eino/schema"
	_ "github.com/eino-contrib/jsonschema"
)
