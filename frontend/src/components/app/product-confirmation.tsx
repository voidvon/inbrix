import { LoaderCircle } from "lucide-react";
import type { Copy } from "../../lib/locale";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "../ui/alert-dialog";

export function ProductActionConfirmation({ copy, open, title, description, pending = false, onOpenChange, onConfirm }: {
  copy: Copy; open: boolean; title: string; description: string; pending?: boolean; onOpenChange: (open: boolean) => void; onConfirm: () => void;
}) {
  return <AlertDialog open={open} onOpenChange={(value, details) => { if (pending) details.cancel(); else onOpenChange(value); }}>
    <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>{title}</AlertDialogTitle><AlertDialogDescription>{description}</AlertDialogDescription></AlertDialogHeader>
      <AlertDialogFooter><AlertDialogCancel disabled={pending}>{copy.cancel}</AlertDialogCancel><AlertDialogAction variant="destructive" disabled={pending} onClick={onConfirm}>{pending && <LoaderCircle className="animate-spin" />}{copy.productDelete}</AlertDialogAction></AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>;
}
