(() => {
  let theme;
  try {
    theme = window.localStorage.getItem("inbrix-theme");
  } catch {
    // Storage may be unavailable; fall back to the system preference.
  }
  const darkMode = theme === "dark" || (
    theme !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches
  );
  document.documentElement.classList.toggle("dark", darkMode);
})();
