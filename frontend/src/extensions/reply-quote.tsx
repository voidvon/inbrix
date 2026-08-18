import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewContent, NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { ChevronDown, ChevronRight, Trash2 } from "lucide-react";

function ReplyQuoteView({ node, updateAttributes, deleteNode }: NodeViewProps) {
  const collapsed = Boolean(node.attrs.collapsed);
  const attribution = String(node.attrs.attribution || "Quoted message");
  return (
    <NodeViewWrapper className="reply-quote" data-collapsed={collapsed ? "true" : "false"}>
      <div className="reply-quote-toolbar" contentEditable={false}>
        <button type="button" onClick={() => updateAttributes({ collapsed: !collapsed })} aria-label={collapsed ? "Expand quoted message" : "Collapse quoted message"} title={collapsed ? "Expand quoted message" : "Collapse quoted message"}>
          {collapsed ? <ChevronRight /> : <ChevronDown />}
          <span>{attribution}</span>
        </button>
        <button type="button" className="reply-quote-delete" onClick={deleteNode} aria-label="Delete quoted message" title="Delete quoted message"><Trash2 /></button>
      </div>
      <NodeViewContent className="reply-quote-content" />
    </NodeViewWrapper>
  );
}

export const ReplyQuote = Node.create({
  name: "replyQuote",
  group: "block",
  content: "block+",
  defining: true,
  isolating: true,

  addAttributes() {
    return {
      attribution: { default: "Quoted message" },
      collapsed: { default: true, rendered: false },
    };
  },

  parseHTML() {
    return [
      {
        tag: "div[data-inbrix-reply-quote]",
        getAttrs: (element) => ({ attribution: element.dataset.attribution || "Quoted message", collapsed: true }),
      },
      {
        tag: "blockquote",
        getAttrs: (element) => ({ attribution: element.dataset.attribution || "Quoted message", collapsed: true }),
      },
    ];
  },

  renderHTML({ HTMLAttributes }) {
    const attribution = typeof HTMLAttributes.attribution === "string" ? HTMLAttributes.attribution : "Quoted message";
    return ["div", mergeAttributes(HTMLAttributes, { "data-inbrix-reply-quote": "", "data-attribution": attribution }), 0];
  },

  addNodeView() {
    return ReactNodeViewRenderer(ReplyQuoteView);
  },
});
