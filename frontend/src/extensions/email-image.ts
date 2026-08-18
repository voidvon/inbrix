import Image from "@tiptap/extension-image";

// Keep the browser-only object URL associated with its staged CID image while
// editing. The data attribute is removed when compose HTML is serialized.
export const EmailImage = Image.extend({
  addAttributes() {
    return {
      ...this.parent?.(),
      inlineImageId: {
        default: null,
        parseHTML: (element) => element.getAttribute("data-inline-image-id"),
        renderHTML: (attributes: Record<string, unknown>) => typeof attributes.inlineImageId === "string" && attributes.inlineImageId
          ? { "data-inline-image-id": attributes.inlineImageId }
          : {},
      },
    };
  },
  addNodeView() {
    const parentNodeView = this.parent?.();
    if (!parentNodeView) return null;

    return (props) => {
      const nodeView = parentNodeView(props);
      const container = nodeView.dom;
      const wrapper = container.querySelector<HTMLElement>("[data-resize-wrapper]");
      const image = wrapper?.querySelector<HTMLImageElement>("img");
      if (!wrapper || !image) return nodeView;

      const size = document.createElement("output");
      size.dataset.resizeSize = "";
      size.setAttribute("aria-hidden", "true");
      wrapper.appendChild(size);

      const updateSize = () => {
        const width = Math.round(Number.parseFloat(image.style.width) || image.offsetWidth);
        const height = Math.round(Number.parseFloat(image.style.height) || image.offsetHeight);
        size.textContent = `${width} x ${height}`;
      };
      updateSize();

      const observer = new MutationObserver(updateSize);
      observer.observe(image, { attributes: true, attributeFilter: ["style", "width", "height"] });

      const destroy = nodeView.destroy?.bind(nodeView);
      nodeView.destroy = () => {
        observer.disconnect();
        destroy?.();
      };
      return nodeView;
    };
  },
}).configure({
  inline: true,
  allowBase64: false,
  resize: {
    enabled: true,
    directions: ["bottom-right"],
    minWidth: 32,
    minHeight: 32,
    alwaysPreserveAspectRatio: true,
  },
});
