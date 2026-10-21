import { useState, type FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { startConversation } from "../../lib/api";
import { isValidRecipient } from "../../lib/email-format";
import type { Copy } from "../../lib/locale";
import { toast, toastError } from "../../lib/toast";
import type { ConversationDetail } from "../../types";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";

export function StartConversationDialog({ copy, accountEmail, initialTo, onClose, onStarted }: {
  copy: Copy;
  accountEmail: string;
  initialTo: string;
  onClose: () => void;
  onStarted: (conversation: ConversationDetail) => Promise<void>;
}) {
  const [to, setTo] = useState(initialTo);
  const [invalid, setInvalid] = useState(false);
  const mutation = useMutation({
    mutationFn: async (recipient: string) => {
      const { conversation } = await startConversation(accountEmail, recipient);
      await onStarted(conversation);
    },
    onError: (value) => toastError(value, copy.loadFailed),
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (mutation.isPending) return;
    if (!isValidRecipient(to.trim())) {
      setInvalid(true);
      toast.error(copy.invalidRecipient);
      return;
    }
    setInvalid(false);
    mutation.mutate(to.trim());
  };
  return (
    <Dialog open onOpenChange={(open) => { if (!open && !mutation.isPending) onClose(); }}>
      <DialogContent data-testid="start-conversation-dialog" className="sm:max-w-md" showCloseButton={!mutation.isPending}>
        <DialogHeader>
          <DialogTitle>{copy.compose}</DialogTitle>
          <DialogDescription>{copy.composeRecipientDescription}</DialogDescription>
        </DialogHeader>
        <form className="grid gap-4" noValidate onSubmit={submit}>
          <div className="grid gap-2">
            <Label htmlFor="compose-recipient">{copy.to}</Label>
            <Input id="compose-recipient" type="email" autoComplete="email" autoFocus required value={to} disabled={mutation.isPending} onChange={(event) => { setTo(event.target.value); setInvalid(false); }} aria-invalid={invalid} />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" disabled={mutation.isPending} onClick={onClose}>{copy.cancel}</Button>
            <Button type="submit" disabled={mutation.isPending || !accountEmail || !to.trim()}>{mutation.isPending ? copy.composeStarting : copy.composeStart}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
