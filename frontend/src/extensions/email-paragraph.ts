import { mergeAttributes } from "@tiptap/core";
import { Paragraph } from "@tiptap/extension-paragraph";

// Keep the content hole even when a paragraph is empty so ProseMirror can place
// the selection in a newly created paragraph. Email-only <br> tags are added
// later, when the editor HTML is serialized for storage or sending.
export const EmailParagraph = Paragraph.extend({
  renderHTML({ HTMLAttributes }) {
    return ["p", mergeAttributes(this.options.HTMLAttributes, HTMLAttributes), 0];
  },
});
