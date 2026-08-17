import { Node, mergeAttributes } from "@tiptap/core";

export const EmailSignature = Node.create({
  name: "emailSignature",
  group: "block",
  content: "block+",
  defining: true,
  isolating: true,

  addAttributes() {
    return {
      signatureId: { default: "", rendered: false },
      signatureName: { default: "", rendered: false },
    };
  },

  parseHTML() {
    return [{
      tag: "div[data-lilmail-signature]",
      getAttrs: (element) => ({
        signatureId: element.getAttribute("data-lilmail-signature") || "",
        signatureName: element.getAttribute("data-signature-name") || "",
      }),
    }];
  },

  renderHTML({ HTMLAttributes, node }) {
    const attributes = node.attrs as Record<string, unknown>;
    const signatureId = typeof attributes.signatureId === "string" ? attributes.signatureId : "";
    const signatureName = typeof attributes.signatureName === "string" ? attributes.signatureName : "";
    return ["div", mergeAttributes(HTMLAttributes, {
      "data-lilmail-signature": signatureId,
      "data-signature-name": signatureName,
    }), 0];
  },
});
