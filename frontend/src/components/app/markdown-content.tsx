import React, { useMemo, useState } from "react";
import { marked, type Token, type Tokens } from "marked";
import DOMPurify from "dompurify";
import { Check, Copy } from "lucide-react";
import { toast } from "sonner";
import { cn } from "../../lib/utils";
import { copyToClipboard } from "../../lib/email-format";

export interface MarkdownContentProps {
  content: string;
  className?: string;
  copyLabels?: {
    copy?: string;
    copied?: string;
  };
}

export function MarkdownContent({
  content,
  className,
  copyLabels,
}: MarkdownContentProps) {
  const tokens = useMemo(() => {
    if (!content) return [];
    try {
      return marked.lexer(content, { gfm: true, breaks: true });
    } catch {
      return [];
    }
  }, [content]);

  if (!content) return null;

  if (tokens.length === 0) {
    return <span className={cn("whitespace-pre-wrap", className)}>{content}</span>;
  }

  return (
    <div
      className={cn(
        "markdown-content text-xs sm:text-sm leading-relaxed text-foreground break-words select-text",
        className
      )}
    >
      {renderTokens(tokens, copyLabels)}
    </div>
  );
}

function renderTokens(
  tokens: Token[],
  copyLabels?: { copy?: string; copied?: string }
): React.ReactNode[] {
  return tokens.map((token, index) => renderToken(token, index, copyLabels));
}

function renderToken(
  token: Token,
  index: number,
  copyLabels?: { copy?: string; copied?: string }
): React.ReactNode {
  const key = `${token.type}-${index}`;

  switch (token.type) {
    case "heading": {
      const headingToken = token as Tokens.Heading;
      const children = renderTokens(headingToken.tokens, copyLabels);
      switch (headingToken.depth) {
        case 1:
          return (
            <h1
              key={key}
              className="my-3 text-base sm:text-lg font-bold text-foreground border-b border-border/60 pb-1.5 first:mt-0"
            >
              {children}
            </h1>
          );
        case 2:
          return (
            <h2
              key={key}
              className="my-2.5 text-sm sm:text-base font-bold text-foreground first:mt-0"
            >
              {children}
            </h2>
          );
        case 3:
          return (
            <h3
              key={key}
              className="my-2 text-xs sm:text-sm font-semibold text-foreground first:mt-0"
            >
              {children}
            </h3>
          );
        default:
          return (
            <h4
              key={key}
              className="my-1.5 text-xs font-semibold text-foreground first:mt-0"
            >
              {children}
            </h4>
          );
      }
    }

    case "paragraph": {
      const pToken = token as Tokens.Paragraph;
      return (
        <p key={key} className="my-1.5 first:mt-0 last:mb-0 leading-relaxed">
          {renderTokens(pToken.tokens, copyLabels)}
        </p>
      );
    }

    case "text": {
      const textToken = token as Tokens.Text;
      if (textToken.tokens && textToken.tokens.length > 0) {
        return <React.Fragment key={key}>{renderTokens(textToken.tokens, copyLabels)}</React.Fragment>;
      }
      return <React.Fragment key={key}>{textToken.text}</React.Fragment>;
    }

    case "strong": {
      const strongToken = token as Tokens.Strong;
      return (
        <strong key={key} className="font-semibold text-foreground">
          {renderTokens(strongToken.tokens, copyLabels)}
        </strong>
      );
    }

    case "em": {
      const emToken = token as Tokens.Em;
      return (
        <em key={key} className="italic">
          {renderTokens(emToken.tokens, copyLabels)}
        </em>
      );
    }

    case "codespan": {
      const codeToken = token as Tokens.Codespan;
      return (
        <code
          key={key}
          className="mx-0.5 rounded border border-border/70 bg-muted/80 px-1.5 py-0.5 font-mono text-[0.88em] font-medium text-foreground"
        >
          {codeToken.text}
        </code>
      );
    }

    case "code": {
      const codeToken = token as Tokens.Code;
      return (
        <CodeBlock
          key={key}
          code={codeToken.text}
          lang={codeToken.lang}
          copyLabels={copyLabels}
        />
      );
    }

    case "blockquote": {
      const bqToken = token as Tokens.Blockquote;
      return (
        <blockquote
          key={key}
          className="my-2.5 border-l-3 border-primary/50 bg-muted/20 px-3 py-1.5 italic text-muted-foreground rounded-r text-xs sm:text-sm"
        >
          {renderTokens(bqToken.tokens, copyLabels)}
        </blockquote>
      );
    }

    case "list": {
      const listToken = token as Tokens.List;
      if (listToken.ordered) {
        return (
          <ol
            key={key}
            start={typeof listToken.start === "number" ? listToken.start : undefined}
            className="my-1.5 pl-5 list-decimal space-y-1 first:mt-0 last:mb-0"
          >
            {listToken.items.map((item, itemIdx) =>
              renderListItem(item, `${key}-${itemIdx}`, copyLabels)
            )}
          </ol>
        );
      }
      return (
        <ul key={key} className="my-1.5 pl-5 list-disc space-y-1 first:mt-0 last:mb-0">
          {listToken.items.map((item, itemIdx) =>
            renderListItem(item, `${key}-${itemIdx}`, copyLabels)
          )}
        </ul>
      );
    }

    case "table": {
      const tableToken = token as Tokens.Table;
      return (
        <div
          key={key}
          className="my-2.5 w-full overflow-x-auto rounded-lg border border-border/80 bg-background/60 shadow-2xs"
        >
          <table className="w-full border-collapse text-left text-xs">
            <thead className="border-b border-border/70 bg-muted/60 font-semibold text-foreground">
              <tr>
                {tableToken.header.map((cell, cellIdx) => {
                  const align = tableToken.align[cellIdx];
                  return (
                    <th
                      key={cellIdx}
                      className={cn(
                        "px-3 py-2 border-r border-border/40 last:border-r-0 font-medium whitespace-nowrap",
                        align === "center" && "text-center",
                        align === "right" && "text-right",
                        (!align || align === "left") && "text-left"
                      )}
                    >
                      {cell.tokens ? renderTokens(cell.tokens, copyLabels) : cell.text}
                    </th>
                  );
                })}
              </tr>
            </thead>
            <tbody className="divide-y divide-border/40">
              {tableToken.rows.map((row, rowIdx) => (
                <tr key={rowIdx} className="hover:bg-muted/30 transition-colors">
                  {row.map((cell, cellIdx) => {
                    const align = tableToken.align[cellIdx];
                    return (
                      <td
                        key={cellIdx}
                        className={cn(
                          "px-3 py-1.5 border-r border-border/40 last:border-r-0 leading-normal",
                          align === "center" && "text-center",
                          align === "right" && "text-right",
                          (!align || align === "left") && "text-left"
                        )}
                      >
                        {cell.tokens ? renderTokens(cell.tokens, copyLabels) : cell.text}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    }

    case "del": {
      const delToken = token as Tokens.Del;
      return (
        <del key={key} className="line-through opacity-75">
          {renderTokens(delToken.tokens, copyLabels)}
        </del>
      );
    }

    case "link": {
      const linkToken = token as Tokens.Link;
      return (
        <a
          key={key}
          href={linkToken.href}
          title={linkToken.title || undefined}
          target="_blank"
          rel="noopener noreferrer"
          className="font-medium text-primary underline underline-offset-3 hover:opacity-85 break-all"
          onClick={(e) => e.stopPropagation()}
        >
          {linkToken.tokens ? renderTokens(linkToken.tokens, copyLabels) : linkToken.text}
        </a>
      );
    }

    case "image": {
      const imgToken = token as Tokens.Image;
      return (
        <img
          key={key}
          src={imgToken.href}
          alt={imgToken.text}
          title={imgToken.title || undefined}
          className="my-2 max-w-full rounded-md shadow-2xs"
          loading="lazy"
        />
      );
    }

    case "hr":
      return <hr key={key} className="my-3 border-border/60" />;

    case "br":
      return <br key={key} />;

    case "space":
      return null;

    case "def":
      return null;

    case "escape": {
      const escToken = token as Tokens.Escape;
      return <React.Fragment key={key}>{escToken.text}</React.Fragment>;
    }

    case "html": {
      const htmlToken = token as Tokens.HTML;
      const raw = htmlToken.text;
      if (/^<br\s*\/?>$/i.test(raw.trim())) {
        return <br key={key} />;
      }
      if (/^<hr\s*\/?>$/i.test(raw.trim())) {
        return <hr key={key} className="my-3 border-border/60" />;
      }
      const clean = DOMPurify.sanitize(raw);
      if (!clean.trim()) return null;
      return (
        <span
          key={key}
          dangerouslySetInnerHTML={{ __html: clean }}
        />
      );
    }

    default:
      if ("tokens" in token && Array.isArray((token as { tokens?: Token[] }).tokens)) {
        return <React.Fragment key={key}>{renderTokens((token as { tokens: Token[] }).tokens, copyLabels)}</React.Fragment>;
      }
      if ("text" in token && typeof (token as { text?: unknown }).text === "string") {
        return <React.Fragment key={key}>{(token as { text: string }).text}</React.Fragment>;
      }
      return null;
  }
}

function renderListItem(
  item: Tokens.ListItem,
  key: string,
  copyLabels?: { copy?: string; copied?: string }
): React.ReactNode {
  if (item.task) {
    return (
      <li key={key} className="flex items-start gap-2 list-none my-0.5">
        <input
          type="checkbox"
          checked={Boolean(item.checked)}
          readOnly
          className="mt-1 size-3.5 rounded border-border pointer-events-none accent-primary"
        />
        <div className="flex-1 min-w-0 leading-relaxed">
          {renderTokens(item.tokens, copyLabels)}
        </div>
      </li>
    );
  }

  return (
    <li key={key} className="leading-relaxed">
      {renderTokens(item.tokens, copyLabels)}
    </li>
  );
}

export function CodeBlock({
  code,
  lang,
  copyLabels,
}: {
  code: string;
  lang?: string;
  copyLabels?: { copy?: string; copied?: string };
}) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    const ok = await copyToClipboard(code);
    if (ok) {
      setCopied(true);
      toast.success(copyLabels?.copied || "代码已复制到剪贴板");
      window.setTimeout(() => setCopied(false), 2000);
    }
  };

  const normalizedLang = lang?.trim().toLowerCase();

  return (
    <div className="my-2.5 overflow-hidden rounded-lg border border-border/80 bg-muted/40 shadow-2xs">
      <div className="flex items-center justify-between border-b border-border/50 bg-muted/70 px-3 py-1.5 text-[11px] text-muted-foreground select-none">
        <span className="font-mono font-medium tracking-wide">
          {normalizedLang || "code"}
        </span>
        <button
          type="button"
          onClick={(e) => {
            void handleCopy(e);
          }}
          className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] transition-colors hover:bg-background/80 hover:text-foreground cursor-pointer"
          title={copyLabels?.copy || "复制代码"}
        >
          {copied ? (
            <>
              <Check className="size-3 text-emerald-600 dark:text-emerald-400" />
              <span className="text-emerald-600 dark:text-emerald-400 font-medium">
                {copyLabels?.copied || "已复制"}
              </span>
            </>
          ) : (
            <>
              <Copy className="size-3" />
              <span>{copyLabels?.copy || "复制"}</span>
            </>
          )}
        </button>
      </div>
      <pre className="overflow-x-auto p-3 font-mono text-xs leading-relaxed text-foreground select-text">
        <code>{code}</code>
      </pre>
    </div>
  );
}
