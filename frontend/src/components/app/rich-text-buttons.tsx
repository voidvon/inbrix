import type { Editor } from "@tiptap/core";
import { Bold, Italic, Link, List, ListOrdered, Underline } from "lucide-react";
import { Button } from "../ui/button";
import { Separator } from "../ui/separator";

export function RichTextButtons({ editor, disabled = false }: { editor: Editor | null; disabled?: boolean }) {
  return (
    <>
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => editor?.chain().focus().toggleBold().run()} aria-label="Bold" title="Bold"><Bold /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => editor?.chain().focus().toggleItalic().run()} aria-label="Italic" title="Italic"><Italic /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => editor?.chain().focus().toggleUnderline().run()} aria-label="Underline" title="Underline"><Underline /></Button>
      <Separator orientation="vertical" className="mx-1 h-5" />
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => editor?.chain().focus().toggleBulletList().run()} aria-label="Bullet list" title="Bullet list"><List /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => editor?.chain().focus().toggleOrderedList().run()} aria-label="Numbered list" title="Numbered list"><ListOrdered /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => { const href = window.prompt("URL"); if (href) editor?.chain().focus().setLink({ href }).run(); }} aria-label="Link" title="Link"><Link /></Button>
    </>
  );
}
