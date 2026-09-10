package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/cloudwego/eino/schema"

	"github.com/mattsp1290/eino-agui/convert"
	"github.com/mattsp1290/eino-agui/emitter"
)

func main() {
	writer := bufio.NewWriter(os.Stdout)
	defer func() {
		if err := writer.Flush(); err != nil {
			panic(err)
		}
	}()
	emit := emitter.NewObserverEmitter(context.Background(), writer, sse.NewSSEWriter())

	base := convert.AgenticIdentityV1{
		SessionID: "demo-session", ThreadID: "demo-session", RunID: "demo-run",
		TurnID: "turn-1", MessageID: "message-1", AttemptID: "attempt-1",
		AgentPath: []convert.AgentPathSegment{{Name: "root", RunID: "demo-run"}},
	}
	runStart := &convert.AgenticEnvelopeV1{Version: 1, Kind: convert.EnvelopeRunStarted, Identity: base, Lifecycle: &convert.LifecycleFactV1{Detail: "admitted"}}
	emit.RunStartedCommitted(*runStart.Lifecycle, receipt(runStart, "revision-1"))

	for turn := 1; turn <= 2; turn++ {
		identity := base
		identity.TurnID = fmt.Sprintf("turn-%d", turn)
		identity.MessageID = fmt.Sprintf("message-%d", turn)
		identity.AttemptID = fmt.Sprintf("attempt-%d", turn)
		started := &convert.AgenticEnvelopeV1{Version: 1, Kind: convert.EnvelopeTurnStarted, Identity: identity, Lifecycle: &convert.LifecycleFactV1{Detail: "admitted"}}
		emit.TurnStarted(*started.Lifecycle, receipt(started, fmt.Sprintf("revision-%d-start", turn)))

		message := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: fmt.Sprintf("turn %d response", turn)})}}
		projection, err := convert.ToAgenticProjection(message, convert.AgenticProjectionContext{Identity: identity, Blocks: []convert.AgenticBlockContext{{BlockID: fmt.Sprintf("block-%d", turn)}}, Limits: convert.DefaultProjectionLimits()})
		must(err)
		emit.EmitCommittedProjection(projection, convert.CommitReceiptV1{Revision: fmt.Sprintf("revision-%d-content", turn), Domain: "projection", Identity: projection.Public.Identity, Digest: projection.Digest}, emitter.DeliveryModeCommittedOnly)

		finished := &convert.AgenticEnvelopeV1{Version: 1, Kind: convert.EnvelopeTurnFinished, Identity: identity, Lifecycle: &convert.LifecycleFactV1{Detail: "committed"}}
		emit.TurnFinished(*finished.Lifecycle, receipt(finished, fmt.Sprintf("revision-%d-finish", turn)))
		base = identity
	}

	child := base
	child.AgentPath = append(append([]convert.AgentPathSegment(nil), child.AgentPath...), convert.AgentPathSegment{Name: "research", RunID: "child-run"})
	childStart := &convert.AgenticEnvelopeV1{Version: 1, Kind: convert.EnvelopeSubagentStarted, Identity: child, Lifecycle: &convert.LifecycleFactV1{Detail: "delegated"}}
	emit.SubagentStartedCommitted(*childStart.Lifecycle, receipt(childStart, "revision-child-start"))
	media, err := convert.ToAgenticProjection(&schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenImage{URL: "https://example.invalid/generated.png", MIMEType: "image/png"})}}, convert.AgenticProjectionContext{Identity: child, Blocks: []convert.AgenticBlockContext{{BlockID: "generated-image"}}, Limits: convert.DefaultProjectionLimits()})
	must(err)
	emit.EmitCommittedProjection(media, convert.CommitReceiptV1{Revision: "revision-child-content", Domain: "projection", Identity: media.Public.Identity, Digest: media.Digest}, emitter.DeliveryModeCommittedOnly)
	childFinished := &convert.AgenticEnvelopeV1{Version: 1, Kind: convert.EnvelopeSubagentFinished, Identity: child, Lifecycle: &convert.LifecycleFactV1{Detail: "committed"}}
	emit.SubagentFinishedCommitted(*childFinished.Lifecycle, receipt(childFinished, "revision-child-finish"))

	targets := []convert.InterruptTargetV1{{ID: "interrupt-1", Address: "agent:root;tool:approval"}}
	paused := &convert.AgenticEnvelopeV1{Version: 1, Kind: convert.EnvelopePaused, Identity: base, Paused: &convert.PausedV1{PauseID: "pause-1", Targets: targets}}
	emit.Paused(*paused.Paused, receipt(paused, "revision-pause"))
	resumed := &convert.AgenticEnvelopeV1{Version: 1, Kind: convert.EnvelopeResumed, Identity: base, Resumed: &convert.ResumedV1{PauseID: "pause-1", Targets: targets, Full: true, NewTurnID: "turn-3", NewAttemptID: "attempt-3"}}
	emit.Resumed(*resumed.Resumed, receipt(resumed, "revision-resume"))

	runFinished := &convert.AgenticEnvelopeV1{Version: 1, Kind: convert.EnvelopeRunFinished, Identity: base, Lifecycle: &convert.LifecycleFactV1{Detail: "settled", LoopSettled: true}}
	emit.RunFinishedCommitted(*runFinished.Lifecycle, receipt(runFinished, "revision-run-finish"))
	if emit.Err() != nil || emit.EncErr() != nil {
		panic(fmt.Errorf("emission failed: %w", errors.Join(emit.Err(), emit.EncErr())))
	}
}

func receipt(envelope *convert.AgenticEnvelopeV1, revision string) convert.CommitReceiptV1 {
	digest, err := convert.LifecycleDigestV1(envelope)
	must(err)
	envelope.Digest = digest
	return convert.CommitReceiptV1{Revision: revision, Domain: "lifecycle", Kind: envelope.Kind, Identity: envelope.Identity, Digest: digest}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
