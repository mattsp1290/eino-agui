package convert

import (
	"encoding/json"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/cloudwego/eino/schema"
)

const (
	AgenticCustomEventName = "eino.agentic.v1"
	AgenticSchemaVersion   = 1
)

type AgentPathSegment struct {
	Name  string `json:"name"`
	RunID string `json:"runId"`
}

type AgenticIdentityV1 struct {
	SessionID string             `json:"sessionId"`
	ThreadID  string             `json:"threadId"`
	RunID     string             `json:"runId"`
	TurnID    string             `json:"turnId"`
	MessageID string             `json:"messageId"`
	BlockID   string             `json:"blockId,omitempty"`
	AttemptID string             `json:"attemptId"`
	CallID    string             `json:"callId,omitempty"`
	AgentPath []AgentPathSegment `json:"agentPath"`
	Transient bool               `json:"transient,omitempty"`
}

type ApprovalInterruptCorrelation struct {
	ApprovalRequestID string `json:"approvalRequestId"`
	InterruptTargetID string `json:"interruptTargetId"`
	InterruptAddress  string `json:"interruptAddress"`
}

type AgenticBlockContext struct {
	BlockID                      string                        `json:"blockId"`
	ProviderServerID             string                        `json:"providerServerId,omitempty"`
	ExpectedApprovalRequestID    string                        `json:"expectedApprovalRequestId,omitempty"`
	ApprovalInterruptCorrelation *ApprovalInterruptCorrelation `json:"approvalInterruptCorrelation,omitempty"`
}

type ProjectionLimits struct {
	MaxMessageBytes      int `json:"maxMessageBytes"`
	MaxBlockBytes        int `json:"maxBlockBytes"`
	MaxBlocks            int `json:"maxBlocks"`
	MaxStreamChunks      int `json:"maxStreamChunks"`
	MaxToolDefinitions   int `json:"maxToolDefinitions"`
	MaxAnnotations       int `json:"maxAnnotations"`
	MaxInterruptTargets  int `json:"maxInterruptTargets"`
	MaxAgentPathSegments int `json:"maxAgentPathSegments"`
	MaxJSONDepth         int `json:"maxJsonDepth"`
	MaxJSONEntries       int `json:"maxJsonEntries"`
}

func DefaultProjectionLimits() ProjectionLimits {
	return ProjectionLimits{
		MaxMessageBytes: 8 << 20, MaxBlockBytes: 1 << 20, MaxBlocks: 1024,
		MaxStreamChunks:    1 << 20,
		MaxToolDefinitions: 1024, MaxAnnotations: 4096, MaxInterruptTargets: 256,
		MaxAgentPathSegments: 64, MaxJSONDepth: 64, MaxJSONEntries: 4096,
	}
}

type AgenticProjectionContext struct {
	Identity AgenticIdentityV1     `json:"identity"`
	Blocks   []AgenticBlockContext `json:"blocks"`
	Limits   ProjectionLimits      `json:"limits"`
}

type CandidateDigestV1 string

type CommitReceiptV1 struct {
	Revision string              `json:"revision"`
	Domain   string              `json:"domain"`
	Kind     AgenticEnvelopeKind `json:"kind,omitempty"`
	Identity AgenticIdentityV1   `json:"identity"`
	Digest   CandidateDigestV1   `json:"digest"`
}

type PublicMedia struct {
	URL        string                `json:"url,omitempty"`
	Base64Data string                `json:"base64Data,omitempty"`
	MIMEType   string                `json:"mimeType,omitempty"`
	Name       string                `json:"name,omitempty"`
	Detail     schema.ImageURLDetail `json:"detail,omitempty"`
}

type PublicParameterInfo struct {
	Type      schema.DataType                 `json:"type"`
	ElemInfo  *PublicParameterInfo            `json:"elemInfo,omitempty"`
	SubParams map[string]*PublicParameterInfo `json:"subParams,omitempty"`
	Desc      string                          `json:"description,omitempty"`
	Enum      []string                        `json:"enum,omitempty"`
	Required  bool                            `json:"required,omitempty"`
}

type PublicToolDefinition struct {
	Name        string                          `json:"name"`
	Description string                          `json:"description,omitempty"`
	ParamsKind  string                          `json:"paramsKind"`
	Params      map[string]*PublicParameterInfo `json:"params,omitempty"`
	JSONSchema  json.RawMessage                 `json:"jsonSchema,omitempty"`
}

type PublicFunctionResultPart struct {
	Type  schema.FunctionToolResultContentBlockType `json:"type"`
	Text  string                                    `json:"text,omitempty"`
	Media *PublicMedia                              `json:"media,omitempty"`
}

type PublicToolSearchResult struct {
	CallID string                 `json:"callId"`
	Name   string                 `json:"name"`
	Tools  []PublicToolDefinition `json:"tools"`
}

type PublicFunctionToolCall struct {
	CallID    string `json:"callId"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type PublicFunctionToolResult struct {
	CallID  string                     `json:"callId"`
	Name    string                     `json:"name"`
	Content []PublicFunctionResultPart `json:"content"`
}
type PublicServerToolCall struct {
	ProviderServerID string `json:"providerServerId"`
	CallID           string `json:"callId"`
	Name             string `json:"name"`
	Arguments        any    `json:"arguments,omitempty"`
}
type PublicServerToolResult struct {
	ProviderServerID string `json:"providerServerId"`
	CallID           string `json:"callId"`
	Name             string `json:"name"`
	Content          any    `json:"content,omitempty"`
}
type PublicMCPToolCall struct {
	ServerLabel       string `json:"serverLabel"`
	ApprovalRequestID string `json:"approvalRequestId,omitempty"`
	CallID            string `json:"callId"`
	Name              string `json:"name"`
	Arguments         string `json:"arguments"`
}
type PublicMCPError struct {
	Code    *int64 `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
type PublicMCPToolResult struct {
	ServerLabel string          `json:"serverLabel"`
	CallID      string          `json:"callId"`
	Name        string          `json:"name"`
	Content     string          `json:"content"`
	Error       *PublicMCPError `json:"error,omitempty"`
}
type PublicMCPListToolsItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}
type PublicMCPListToolsResult struct {
	ServerLabel string                   `json:"serverLabel"`
	Tools       []PublicMCPListToolsItem `json:"tools"`
	Error       string                   `json:"error,omitempty"`
}
type PublicMCPApprovalRequest struct {
	ID          string `json:"id"`
	ServerLabel string `json:"serverLabel"`
	Name        string `json:"name"`
	Arguments   string `json:"arguments"`
}
type PublicMCPApprovalResponse struct {
	ApprovalRequestID string `json:"approvalRequestId"`
	Approve           bool   `json:"approve"`
	Reason            string `json:"reason,omitempty"`
}

type PublicOpenAIAnnotation struct {
	Type        string `json:"type"`
	FileID      string `json:"fileId,omitempty"`
	Filename    string `json:"filename,omitempty"`
	ContainerID string `json:"containerId,omitempty"`
	Title       string `json:"title,omitempty"`
	URL         string `json:"url,omitempty"`
	Index       int    `json:"index,omitempty"`
	StartIndex  int    `json:"startIndex,omitempty"`
	EndIndex    int    `json:"endIndex,omitempty"`
}

type PublicClaudeCitation struct {
	Type          string `json:"type"`
	CitedText     string `json:"citedText,omitempty"`
	DocumentTitle string `json:"documentTitle,omitempty"`
	DocumentIndex int    `json:"documentIndex,omitempty"`
	StartIndex    int    `json:"startIndex,omitempty"`
	EndIndex      int    `json:"endIndex,omitempty"`
	Title         string `json:"title,omitempty"`
	URL           string `json:"url,omitempty"`
}

type PublicGeminiGroundingChunk struct {
	Domain string `json:"domain,omitempty"`
	Title  string `json:"title,omitempty"`
	URI    string `json:"uri,omitempty"`
}
type PublicGeminiGroundingSupport struct {
	ConfidenceScores      []float32 `json:"confidenceScores,omitempty"`
	GroundingChunkIndices []int     `json:"groundingChunkIndices"`
	PartIndex             int       `json:"partIndex"`
	StartIndex            int       `json:"startIndex"`
	EndIndex              int       `json:"endIndex"`
	Text                  string    `json:"text,omitempty"`
}
type PublicGeminiGrounding struct {
	Chunks                []PublicGeminiGroundingChunk   `json:"chunks,omitempty"`
	Supports              []PublicGeminiGroundingSupport `json:"supports,omitempty"`
	WebSearchQueries      []string                       `json:"webSearchQueries,omitempty"`
	RenderedSearchContent string                         `json:"renderedSearchContent,omitempty"`
}

type PublicProviderAnnotations struct {
	RefusalReason string                   `json:"refusalReason,omitempty"`
	OpenAI        []PublicOpenAIAnnotation `json:"openai,omitempty"`
	Claude        []PublicClaudeCitation   `json:"claude,omitempty"`
}

type PublicResponseMeta struct {
	TokenUsage             *events.TokenUsage     `json:"tokenUsage,omitempty"`
	OpenAIStatus           string                 `json:"openaiStatus,omitempty"`
	OpenAIError            *PublicProviderError   `json:"openaiError,omitempty"`
	OpenAIIncompleteReason string                 `json:"openaiIncompleteReason,omitempty"`
	ClaudeStopReason       string                 `json:"claudeStopReason,omitempty"`
	ClaudeStopSequence     string                 `json:"claudeStopSequence,omitempty"`
	ClaudeStopCategory     string                 `json:"claudeStopCategory,omitempty"`
	ClaudeStopExplanation  string                 `json:"claudeStopExplanation,omitempty"`
	GeminiFinishReason     string                 `json:"geminiFinishReason,omitempty"`
	GeminiGrounding        *PublicGeminiGrounding `json:"geminiGrounding,omitempty"`
}

type PublicContentBlock struct {
	Type                schema.ContentBlockType    `json:"type"`
	Identity            AgenticIdentityV1          `json:"identity"`
	Text                *string                    `json:"text,omitempty"`
	Media               *PublicMedia               `json:"media,omitempty"`
	ToolSearchResult    *PublicToolSearchResult    `json:"toolSearchResult,omitempty"`
	FunctionToolCall    *PublicFunctionToolCall    `json:"functionToolCall,omitempty"`
	FunctionToolResult  *PublicFunctionToolResult  `json:"functionToolResult,omitempty"`
	ServerToolCall      *PublicServerToolCall      `json:"serverToolCall,omitempty"`
	ServerToolResult    *PublicServerToolResult    `json:"serverToolResult,omitempty"`
	MCPToolCall         *PublicMCPToolCall         `json:"mcpToolCall,omitempty"`
	MCPToolResult       *PublicMCPToolResult       `json:"mcpToolResult,omitempty"`
	MCPListToolsResult  *PublicMCPListToolsResult  `json:"mcpListToolsResult,omitempty"`
	MCPApprovalRequest  *PublicMCPApprovalRequest  `json:"mcpApprovalRequest,omitempty"`
	MCPApprovalResponse *PublicMCPApprovalResponse `json:"mcpApprovalResponse,omitempty"`
	ProviderAnnotations *PublicProviderAnnotations `json:"providerAnnotations,omitempty"`
}

type PublicAgenticMessage struct {
	Version       int                    `json:"version"`
	Identity      AgenticIdentityV1      `json:"identity"`
	Role          schema.AgenticRoleType `json:"role"`
	ContentBlocks []PublicContentBlock   `json:"contentBlocks"`
	ResponseMeta  *PublicResponseMeta    `json:"responseMeta,omitempty"`
	Digest        CandidateDigestV1      `json:"digest,omitempty"`
}

type AgenticEnvelopeKind string

const (
	EnvelopeContentBlock        AgenticEnvelopeKind = "content_block"
	EnvelopeProviderAnnotations AgenticEnvelopeKind = "provider_annotations"
	EnvelopeRunStarted          AgenticEnvelopeKind = "run_started"
	EnvelopeRunFinished         AgenticEnvelopeKind = "run_finished"
	EnvelopeRunError            AgenticEnvelopeKind = "run_error"
	EnvelopeTurnStarted         AgenticEnvelopeKind = "turn_started"
	EnvelopeTurnFinished        AgenticEnvelopeKind = "turn_finished"
	EnvelopeAttemptReplaced     AgenticEnvelopeKind = "attempt_replaced"
	EnvelopeSubagentStarted     AgenticEnvelopeKind = "subagent_started"
	EnvelopeSubagentFinished    AgenticEnvelopeKind = "subagent_finished"
	EnvelopeSubagentError       AgenticEnvelopeKind = "subagent_error"
	EnvelopePaused              AgenticEnvelopeKind = "paused"
	EnvelopeResumed             AgenticEnvelopeKind = "resumed"
	EnvelopeCancelled           AgenticEnvelopeKind = "cancelled"
)

type LifecycleFactV1 struct {
	Detail      string `json:"detail,omitempty"`
	LoopSettled bool   `json:"loopSettled,omitempty"`
}
type PublicProviderError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
type AttemptReplacedV1 struct {
	OldAttemptID string `json:"oldAttemptId"`
	NewAttemptID string `json:"newAttemptId"`
	Cause        string `json:"cause"`
	Semantics    string `json:"semantics"`
}
type InterruptTargetV1 struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}
type PausedV1 struct {
	Targets     []InterruptTargetV1           `json:"targets"`
	Correlation *ApprovalInterruptCorrelation `json:"correlation,omitempty"`
}
type ResumedV1 struct {
	PauseID      string                        `json:"pauseId"`
	Targets      []InterruptTargetV1           `json:"targets"`
	Full         bool                          `json:"full"`
	NewTurnID    string                        `json:"newTurnId"`
	NewAttemptID string                        `json:"newAttemptId"`
	Correlation  *ApprovalInterruptCorrelation `json:"correlation,omitempty"`
}
type CancelledV1 struct {
	RequestedMode  string `json:"requestedMode"`
	ObservedMode   string `json:"observedMode"`
	Classification string `json:"classification"`
}
type TurnStartedV1 = LifecycleFactV1
type TurnFinishedV1 = LifecycleFactV1

type AgenticEnvelopeV1 struct {
	Version             int                        `json:"version"`
	Kind                AgenticEnvelopeKind        `json:"kind"`
	Identity            AgenticIdentityV1          `json:"identity"`
	ContentBlock        *PublicContentBlock        `json:"contentBlock,omitempty"`
	ProviderAnnotations *PublicProviderAnnotations `json:"providerAnnotations,omitempty"`
	Lifecycle           *LifecycleFactV1           `json:"lifecycle,omitempty"`
	AttemptReplaced     *AttemptReplacedV1         `json:"attemptReplaced,omitempty"`
	Paused              *PausedV1                  `json:"paused,omitempty"`
	Resumed             *ResumedV1                 `json:"resumed,omitempty"`
	Cancelled           *CancelledV1               `json:"cancelled,omitempty"`
	Digest              CandidateDigestV1          `json:"digest,omitempty"`
}
