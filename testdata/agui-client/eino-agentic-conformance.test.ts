import { describe, expect, it } from "vitest";
import { firstValueFrom, of, Subject } from "rxjs";
import { toArray } from "rxjs/operators";
import {
  BaseEvent,
  CustomEvent,
  EventType,
  Message,
  RunAgentInput,
  RunStartedEvent,
  TextMessageChunkEvent,
  TextMessageContentEvent,
  TextMessageEndEvent,
  TextMessageStartEvent,
} from "@ag-ui/core";
import { AbstractAgent } from "@/agent";
import { transformChunks } from "@/chunks/transform";
import { defaultApplyEvents } from "../default";

const identity = {
  sessionId: "session",
  threadId: "session",
  runId: "run",
  turnId: "turn",
  messageId: "message",
  blockId: "block",
  attemptId: "attempt",
  agentPath: [{ name: "root", runId: "run" }],
};

const supplement: CustomEvent = {
  type: EventType.CUSTOM,
  name: "eino.agentic.v1",
  value: {
    version: 1,
    kind: "content_block",
    identity,
    contentBlock: { type: "assistant_gen_text", identity, text: "hello" },
  },
};

const runStarted = { type: EventType.RUN_STARTED, threadId: "session", runId: "run" } as RunStartedEvent;

const apply = async (raw: BaseEvent[]): Promise<Message[]> => {
  const transformed = await firstValueFrom(transformChunks()(of(...raw)).pipe(toArray()));
  const input = { messages: [], state: {}, threadId: "session", runId: "run", tools: [], context: [] } as RunAgentInput;
  const agent = { messages: [], state: {} } as unknown as AbstractAgent;
  const stream = new Subject<BaseEvent>();
  const updates = firstValueFrom(defaultApplyEvents(input, stream, agent, []).pipe(toArray()));
  transformed.forEach((event) => stream.next(event));
  stream.complete();
  const all = await updates;
  const messages = [...all].reverse().find((update) => update.messages !== undefined)?.messages ?? [];

  // The stock reducer intentionally ignores namespaced custom semantics. The
  // bridge decoder merges committed identity by message/block ID after native
  // reduction; that replacement clears transient observer metadata without
  // replaying content bytes.
  const committed = transformed
    .filter((event): event is CustomEvent => event.type === EventType.CUSTOM && (event as CustomEvent).name === "eino.agentic.v1")
    .map((event) => event.value?.identity)
    .filter(Boolean);
  return messages.map((message) => {
    const match = committed.find((candidate) => candidate.messageId === message.id);
    if (!match) return message;
    return { ...message, metadata: { ...message.metadata, "eino.agentic.v1": match } };
  });
};

describe("eino.agentic.v1 live/replay conformance", () => {
  it("renders live continuation bytes once and equals fresh replay", async () => {
    const live = await apply([
      runStarted,
      { type: EventType.TEXT_MESSAGE_CHUNK, messageId: "message", role: "assistant", delta: "hello", metadata: { "eino.agentic.v1": { ...identity, transient: true } } } as TextMessageChunkEvent,
      supplement,
    ]);
    const replay = await apply([
      runStarted,
      { type: EventType.TEXT_MESSAGE_START, messageId: "message", role: "assistant", metadata: { "eino.agentic.v1": identity } } as TextMessageStartEvent,
      { type: EventType.TEXT_MESSAGE_CONTENT, messageId: "message", delta: "hello", metadata: { "eino.agentic.v1": identity } } as TextMessageContentEvent,
      { type: EventType.TEXT_MESSAGE_END, messageId: "message", metadata: { "eino.agentic.v1": identity } } as TextMessageEndEvent,
      supplement,
    ]);

    expect(live).toHaveLength(1);
    expect(live[0]).toMatchObject({ id: "message", role: "assistant", content: "hello" });
    expect(replay).toEqual(live);
  });
});
