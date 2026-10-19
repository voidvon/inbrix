import { createContext, useContext } from "react";
import type { Copy } from "../../lib/locale";
import type { ConversationListResponse, Mailbox } from "../../types";
import type { ComposeDefaults } from "../../lib/email-format";

export type ShellContextType = {
  copy: Copy;
  accountEmail: string;
  folders: Mailbox[];
  accounts: ConversationListResponse["accounts"];
  openMobileMenu: () => void;
  openCompose: (defaults?: ComposeDefaults) => void;
  /**
   * The open compose window, if any. A reply renders inside the transcript it
   * answers, so the conversation view needs to know whether the window belongs
   * to it — hence the state, not just the opener.
   */
  compose: { open: boolean; defaults: ComposeDefaults };
  closeCompose: () => void;
  openSettings: () => void;
};

export const ShellContext = createContext<ShellContextType | null>(null);

export function useShell(): ShellContextType {
  const context = useContext(ShellContext);
  if (!context) {
    throw new Error("useShell must be used within AppLayout");
  }
  return context;
}
