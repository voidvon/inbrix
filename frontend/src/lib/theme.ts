export function prefersDarkMode() {
  // The blocking HTML script resolves the saved/system preference before startup.
  return typeof document !== "undefined" && document.documentElement.classList.contains("dark");
}
