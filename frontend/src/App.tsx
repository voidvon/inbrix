import { useEffect, useState, type ReactNode } from "react";
import { zh } from "./lib/locale";
import { AppLayout } from "./components/app/app-layout";
import { LoginScreen, RegisterScreen } from "./components/app/auth-screens";
import { InboxPage } from "./components/app/inbox-page";
import { FolderPage } from "./components/app/folder-page";
import { AttachmentsPage } from "./components/app/attachments-page";
import { DocumentListPage } from "./components/app/document-list-page";
import { CalendarPage } from "./components/app/calendar-page";

export function App() {
  const [path, setPath] = useState(() => window.location.pathname);

  useEffect(() => {
    const syncPath = () => setPath(window.location.pathname);
    const navigateWithinMail = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const target = event.target;
      const anchor = target instanceof Element ? target.closest("a") : null;
      if (!anchor || anchor.target || anchor.hasAttribute("download")) return;
      const url = new URL(anchor.href, window.location.href);
      const isMailRoute =
        url.pathname === "/inbox" ||
        url.pathname === "/attachments" ||
        url.pathname === "/documents" ||
        url.pathname.startsWith("/documents/") ||
        url.pathname === "/calendar" ||
        url.pathname === "/calendar/week" ||
        url.pathname.startsWith("/folder/");
      if (url.origin !== window.location.origin || !isMailRoute) return;
      event.preventDefault();
      if (url.href !== window.location.href) window.history.pushState(window.history.state, "", url);
      syncPath();
    };
    window.addEventListener("popstate", syncPath);
    document.addEventListener("click", navigateWithinMail);
    return () => {
      window.removeEventListener("popstate", syncPath);
      document.removeEventListener("click", navigateWithinMail);
    };
  }, []);

  if (path === "/login" || path === "/user-login") return <LoginScreen copy={zh} />;
  if (path === "/register") return <RegisterScreen copy={zh} />;

  let content: ReactNode;
  if (path === "/settings") {
    content = <InboxPage />;
  } else if (path === "/attachments") {
    content = <AttachmentsPage />;
  } else if (path === "/documents") {
    content = <DocumentListPage />;
  } else if (path === "/documents/new") {
    content = <DocumentListPage key={path} createDocument />;
  } else if (path.startsWith("/documents/")) {
    content = <DocumentListPage key={path} documentId={decodeURIComponent(path.slice("/documents/".length))} />;
  } else if (path === "/calendar" || path === "/calendar/week") {
    content = <CalendarPage />;
  } else if (path.startsWith("/folder/")) {
    content = <FolderPage key={path} folder={decodeURIComponent(path.slice("/folder/".length))} />;
  } else {
    content = <InboxPage />;
  }

  return <AppLayout path={path}>{content}</AppLayout>;
}

export default App;
