import { Toaster } from "../ui/sonner";

/**
 * The app's single toast viewport. The top-center anchor is part of the
 * design, so screens never need to configure a Toaster of their own.
 */
export function AppToaster() {
  return <Toaster position="top-center" />;
}
