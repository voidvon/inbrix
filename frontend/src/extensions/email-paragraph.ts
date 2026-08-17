import { mergeAttributes } from "@tiptap/core";
import { Paragraph } from "@tiptap/extension-paragraph";

// Keep empty paragraphs as empty paragraph nodes in the editor model, but give
// them an explicit line box when Tiptap renders HTML for the editor or email.
export const EmailParagraph = Paragraph.extend({
  renderHTML({ HTMLAttributes, node }) {
    return ["p", mergeAttributes(this.options.HTMLAttributes, HTMLAttributes), node.content.size ? 0 : ["br"]];
  },
});
