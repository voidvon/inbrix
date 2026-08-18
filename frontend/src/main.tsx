import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import App from "./App";
import { Toaster } from "./components/ui/sonner";
import { applyTheme, prefersDarkMode } from "./lib/theme";
import "./styles.css";

// Apply the saved theme before React renders so public pages share the same
// appearance and do not flash the light theme during navigation.
applyTheme(prefersDarkMode());

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
      staleTime: 15_000,
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
      <Toaster position="top-center" />
    </QueryClientProvider>
  </StrictMode>,
);
