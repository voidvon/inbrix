import type { AIAgentChatResponse, AIToolCall } from "./api";

export type AIChatStreamEvent =
  | { type: "started" | "text_reset" }
  | { type: "text_delta"; delta: string }
  | { type: "tool_start" | "tool_end"; tool: AIToolCall }
  | { type: "done"; response: AIAgentChatResponse }
  | { type: "error"; error: string };

/** SSE frames can span network reads, UTF-8 characters and CRLF boundaries. */
export async function readAIChatStream(response: Response, onEvent: (event: AIChatStreamEvent) => void): Promise<AIAgentChatResponse> {
  if (!response.body) throw new Error("AI stream is unavailable");
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let data: string[] = [];
  let frameSize = 0;
  let result: AIAgentChatResponse | undefined;
  const dispatch = () => {
    if (!data.length) return;
    const event = JSON.parse(data.join("\n")) as AIChatStreamEvent;
    data = [];
    frameSize = 0;
    if (event.type === "error") throw new Error(event.error || "AI stream failed");
    if (event.type === "done") {
      if (typeof event.response?.content !== "string") throw new Error("Invalid AI stream result");
      result = event.response;
    }
    onEvent(event);
  };
  const line = (value: string) => {
    if (!value) dispatch();
    else if (value.startsWith("data:")) {
      const field = value.slice(5).replace(/^ /, "");
      frameSize += field.length;
      if (frameSize > 2 * 1024 * 1024) throw new Error("AI stream frame is too large");
      data.push(field);
    }
  };
  try {
    while (!result) {
      const chunk = await reader.read();
      buffer += decoder.decode(chunk.value, { stream: !chunk.done });
      let newline: number;
      while (!result && (newline = buffer.indexOf("\n")) >= 0) {
        line(buffer.slice(0, newline).replace(/\r$/, ""));
        buffer = buffer.slice(newline + 1);
      }
      if (buffer.length > 2 * 1024 * 1024) throw new Error("AI stream line is too large");
      if (chunk.done) {
        if (!result) {
          if (buffer) line(buffer.replace(/\r$/, ""));
          dispatch();
        }
        break;
      }
    }
    if (!result) throw new Error("AI 回复中断，请重试 / AI stream interrupted before completion");
    return result;
  } finally {
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}
