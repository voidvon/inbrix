import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { DOCUMENT_CHANGED_EVENT } from "./document-storage";

// A library save can advance a linked chat's version and invalidate a running
// response. Refresh the authoritative version before the next message.
export function useAIDocumentSync() {
  const client = useQueryClient();
  useEffect(() => {
    const refresh = () => {
      for (const key of ["ai-document-sessions", "ai-document-session", "conversation-ai-chat"]) {
        void client.invalidateQueries({ queryKey: [key] });
      }
    };
    window.addEventListener(DOCUMENT_CHANGED_EVENT, refresh);
    return () => window.removeEventListener(DOCUMENT_CHANGED_EVENT, refresh);
  }, [client]);
}
