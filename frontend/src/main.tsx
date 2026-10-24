import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import App from "./App";
import { AppToaster } from "./components/app/app-toaster";
import { TooltipProvider } from "./components/ui/tooltip";
import "./styles.css";
import { clearLegacyDocumentCache } from "./lib/document-storage";

clearLegacyDocumentCache();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
      staleTime: 15_000,
    },
  },
});

// Query state belongs to the current view only. Always read the server again
// when returning, and discard inactive document responses immediately.
for (const key of ["ai-document-sessions", "ai-document-session", "document-templates", "document-ai-agents", "document-ai-bindings"]) {
  queryClient.setQueryDefaults([key], { staleTime: 0, gcTime: 0, refetchOnMount: "always", retry: false });
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <App />
        <AppToaster />
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
);
