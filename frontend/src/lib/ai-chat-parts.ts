import type { ConversationAIChatMessage, ConversationAIChatPart, ConversationAIToolCall } from "../types";

export function appendAIChatText(parts: ConversationAIChatPart[], delta: string, newBlock: boolean): ConversationAIChatPart[] {
  const last = parts.at(-1);
  if (!newBlock && last?.type === "text") {
    return [...parts.slice(0, -1), { type: "text", content: last.content + delta }];
  }
  return [...parts, { type: "text", content: delta }];
}

export function upsertAIChatTool(parts: ConversationAIChatPart[], tool: ConversationAIToolCall): ConversationAIChatPart[] {
  const index = tool.id ? parts.findIndex((part) => part.type === "tool" && part.tool.id === tool.id) : -1;
  // A result updates the original call row; it never moves it past newer events.
  if (index >= 0) return parts.map((part, i) => i === index ? { type: "tool", tool } : part);
  return [...parts, { type: "tool", tool }];
}

export function getAIChatParts(message: Pick<ConversationAIChatMessage, "parts" | "content" | "toolCalls">): ConversationAIChatPart[] {
  if (message.parts?.length) return message.parts;
  // Older records contain only tools and the final answer. Their intermediate
  // commentary cannot be reconstructed, but tools precede the final synthesis.
  return [
    ...(message.toolCalls ?? []).map((tool): ConversationAIChatPart => ({ type: "tool", tool })),
    ...(message.content ? [{ type: "text" as const, content: message.content }] : []),
  ];
}

export function finishAIChatParts(parts: ConversationAIChatPart[], response: Pick<ConversationAIChatMessage, "parts" | "content" | "toolCalls">): ConversationAIChatPart[] {
  if (response.parts?.length) return response.parts;
  if (!parts.length) return getAIChatParts(response);
  const updated = parts.map((part): ConversationAIChatPart => {
    if (part.type !== "tool") return part;
    const tool = response.toolCalls?.find((call) => call.id && call.id === part.tool.id);
    return tool ? { type: "tool", tool } : part;
  });
  // Compatibility with streaming servers that do not yet return ordered parts.
  if (updated.at(-1)?.type === "text") updated.pop();
  return [...updated, { type: "text", content: response.content }];
}
