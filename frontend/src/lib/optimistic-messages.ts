import { useEffect, useState } from "react";
import type { ConversationMessage } from "../types";
import { sendMessage } from "./api";

export type OptimisticMessage = ConversationMessage & {
  conversationId: string;
  sendStatus: "sending" | "failed" | "sent";
  sendError?: string;
  form?: FormData;
  createdAt: number;
};

type Listener = () => void;
const listeners = new Set<Listener>();

let optimisticStore: OptimisticMessage[] = [];

function notify() {
  for (const listener of listeners) {
    listener();
  }
}

export function getOptimisticMessages(conversationId: string): OptimisticMessage[] {
  return optimisticStore.filter((m) => m.conversationId === conversationId);
}

export function getAllOptimisticMessages(): OptimisticMessage[] {
  return optimisticStore;
}

export function addOptimisticMessage(message: OptimisticMessage) {
  const existingIdx = optimisticStore.findIndex((m) => m.id === message.id);
  if (existingIdx >= 0) {
    optimisticStore[existingIdx] = message;
  } else {
    optimisticStore.push(message);
  }
  notify();
}

export function updateOptimisticStatus(
  id: string,
  sendStatus: "sending" | "failed" | "sent",
  sendError?: string
) {
  const msg = optimisticStore.find((m) => m.id === id);
  if (msg) {
    msg.sendStatus = sendStatus;
    msg.sendError = sendError;
    notify();
  }
}

export function removeOptimisticMessage(id: string) {
  const initialLen = optimisticStore.length;
  optimisticStore = optimisticStore.filter((m) => m.id !== id);
  if (optimisticStore.length !== initialLen) {
    notify();
  }
}

export function reconcileOptimisticMessages(
  conversationId: string,
  serverMessages: ConversationMessage[]
) {
  if (!serverMessages.length) return;
  const initialLen = optimisticStore.length;

  optimisticStore = optimisticStore.filter((optMsg) => {
    if (optMsg.conversationId !== conversationId) return true;
    // Sending and failed messages must never be pruned by server refetches
    if (optMsg.sendStatus !== "sent") return true;

    // If sent, check if server has synced this message
    const matched = serverMessages.some((srv) => {
      if (!srv.outgoing) return false;
      const srvBody = (srv.body || srv.preview || "").trim();
      const optBody = (optMsg.body || optMsg.preview || "").trim();
      if (srvBody && optBody) {
        if (srvBody === optBody || srvBody.includes(optBody) || optBody.includes(srvBody)) {
          return true;
        }
      }
      if (
        srv.subject &&
        optMsg.subject &&
        srv.subject.trim() === optMsg.subject.trim() &&
        Math.abs(new Date(srv.date).getTime() - new Date(optMsg.date).getTime()) < 5 * 60 * 1000
      ) {
        return true;
      }
      return false;
    });

    if (matched) {
      return false;
    }

    // Prune if marked sent for over 5 minutes to avoid memory accumulation
    if (Date.now() - optMsg.createdAt > 5 * 60 * 1000) {
      return false;
    }

    return true;
  });

  if (optimisticStore.length !== initialLen) {
    notify();
  }
}

export async function retrySendMessage(
  id: string,
  onSuccess?: () => void,
  onError?: (err: Error) => void
) {
  const msg = optimisticStore.find((m) => m.id === id);
  if (!msg || !msg.form) return;

  updateOptimisticStatus(id, "sending", undefined);

  try {
    await sendMessage(msg.form);
    updateOptimisticStatus(id, "sent", undefined);
    onSuccess?.();
  } catch (err) {
    const errorMsg = err instanceof Error ? err.message : String(err);
    updateOptimisticStatus(id, "failed", errorMsg);
    onError?.(err instanceof Error ? err : new Error(errorMsg));
  }
}

export function useOptimisticMessages(conversationId?: string | null): OptimisticMessage[] {
  const [, setTick] = useState(0);

  useEffect(() => {
    const onChange = () => setTick((t) => t + 1);
    listeners.add(onChange);
    return () => {
      listeners.delete(onChange);
    };
  }, []);

  if (!conversationId) return [];
  return getOptimisticMessages(conversationId);
}
