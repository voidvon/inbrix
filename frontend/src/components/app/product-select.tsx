import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";

// The application wrapper handles empty values; all interaction and rendering
// use the official shadcn Base UI Select API.
export function ProductSelect({ value, options, onValueChange, label, disabled = false, className = "", id }: {
  value: string; options: { value: string; label: string }[]; onValueChange: (value: string) => void;
  label: string; disabled?: boolean; className?: string; id?: string;
}) {
  const items = options.map(option => ({ ...option, value: `value:${option.value}` }));
  return <Select items={items} value={`value:${value}`} disabled={disabled} onValueChange={next => { if (typeof next === "string") onValueChange(next.slice(6)); }}>
    <SelectTrigger id={id} aria-label={label} className={`w-full min-w-0 ${className}`}><SelectValue /></SelectTrigger>
    <SelectContent>{items.map(option => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent>
  </Select>;
}
