// Ambient browser declarations retained for the service-worker build.
// The React client keeps its own types under frontend/src.
export {};

declare global {
  interface Window {
    inbrixLocale?: string;
  }
}
