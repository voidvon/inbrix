import { useEffect, useRef, useState } from "react";
import { useSaveConversationNote } from "../../lib/conversation-note";
import { toastError } from "../../lib/toast";
import type { Copy } from "../../lib/locale";
import { Textarea } from "../ui/textarea";

export function ConversationNoteEntry({ copy, conversationId, note }: {
  copy: Copy;
  conversationId: string;
  note: string;
}) {
  const [draft, setDraft] = useState(note);
  const draftRef = useRef(note);
  const savedRef = useRef(note);
  const focusedRef = useRef(false);
  const savingRef = useRef(false);
  const saveRequestedRef = useRef(false);
  const saveRef = useRef<() => void>(() => {});
  const mutation = useSaveConversationNote(conversationId);

  useEffect(() => {
    if (mutation.error) toastError(mutation.error, copy.noteSaveFailed);
  }, [mutation.error, copy.noteSaveFailed]);

  useEffect(() => {
    // List edits and background refreshes must not overwrite unsaved typing.
    if (focusedRef.current || savingRef.current || draftRef.current.trim() !== savedRef.current) return;
    savedRef.current = note;
    draftRef.current = note;
    setDraft(note);
  }, [note]);

  const save = async () => {
    if (savingRef.current) {
      saveRequestedRef.current = true;
      return;
    }
    const value = draftRef.current.trim();
    if (value === savedRef.current) return;
    savingRef.current = true;
    try {
      await mutation.mutateAsync(value);
      savedRef.current = value;
      if (!focusedRef.current && draftRef.current.trim() === value) {
        draftRef.current = value;
        setDraft(value);
      }
    } catch {
      // Preserve failed edits and retry on the next blur.
    } finally {
      savingRef.current = false;
      if (saveRequestedRef.current) {
        saveRequestedRef.current = false;
        saveRef.current();
      }
    }
  };
  saveRef.current = () => { void save(); };

  // A route or conversation change can remove a focused textarea without
  // dispatching blur. Flush its latest value under this conversation as well.
  useEffect(() => () => saveRef.current(), []);

  return (
    <div data-testid="conversation-note-entry">
      <Textarea
        className="min-h-24 resize-y bg-card/60"
        rows={3}
        value={draft}
        placeholder={copy.conversationNote}
        aria-label={copy.conversationNote}
        maxLength={200}
        onChange={(event) => {
          draftRef.current = event.target.value;
          setDraft(event.target.value);
        }}
        onFocus={() => { focusedRef.current = true; }}
        onBlur={() => {
          focusedRef.current = false;
          saveRef.current();
        }}
        aria-busy={mutation.isPending}
        aria-invalid={mutation.isError}
      />
    </div>
  );
}
