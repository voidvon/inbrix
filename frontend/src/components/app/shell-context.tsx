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
