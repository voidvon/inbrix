export function prefersDarkMode() {
  if (typeof window === "undefined") return false;
  const stored = window.localStorage.getItem("inbrix-theme");
  if (stored === "dark") return true;
  if (stored === "light") return false;
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export function applyTheme(darkMode: boolean) {
  document.documentElement.classList.toggle("dark", darkMode);
}
