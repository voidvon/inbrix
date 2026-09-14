import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Menu } from "lucide-react";
import { createCalendarEvent, getCalendarEvents } from "../../lib/api";
import { formatTime } from "../../lib/utils";
import { en } from "../../lib/locale";
import type { CalendarEvent } from "../../types";
import { useShell } from "./shell-context";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { ScrollArea } from "../ui/scroll-area";

export function CalendarPage() {
  const queryClient = useQueryClient();
  const { copy, openMobileMenu } = useShell();
  const now = new Date();
  const start = new Date(now.getFullYear(), now.getMonth(), 1).toISOString();
  const end = new Date(now.getFullYear(), now.getMonth() + 1, 1).toISOString();
  const events = useQuery({ queryKey: ["calendar", start], queryFn: () => getCalendarEvents(start, end), retry: false });
  const [form, setForm] = useState({ summary: "", location: "", start: "", end: "" });
  const create = useMutation({
    mutationFn: () => createCalendarEvent({ ...form, start: new Date(form.start).toISOString(), end: new Date(form.end).toISOString(), allDay: false }),
    onSuccess: () => {
      setForm({ summary: "", location: "", start: "", end: "" });
      void queryClient.invalidateQueries({ queryKey: ["calendar"] });
    },
  });

  return (
    <ScrollArea className="min-w-0 flex-1 bg-background" contentClassName="min-h-full" render={<main />}>
      <header className="sticky top-0 z-10 flex h-14 items-center gap-3 border-b bg-card px-3 sm:px-5">
        <Button variant="ghost" size="icon" className="shrink-0 lg:hidden" onClick={openMobileMenu} aria-label={copy.folders} title={copy.folders}>
          <Menu />
        </Button>
        <h1 className="text-sm font-semibold">{copy.calendar}</h1>
      </header>
      <div className="mx-auto grid max-w-5xl gap-8 p-4 py-8 lg:grid-cols-[minmax(0,1fr)_22rem] lg:p-8">
        <section>
          <h2 className="text-lg font-semibold">
            {now.toLocaleDateString(copy === en ? "en" : "zh-CN", { month: "long", year: "numeric" })}
          </h2>
          <div className="mt-5 grid gap-2">
            {events.data?.events.map((event: CalendarEvent) => (
              <article className="grid grid-cols-[6rem_minmax(0,1fr)] gap-4 border-b py-3" key={event.uid}>
                <time className="text-xs text-muted-foreground">{formatTime(event.start)}</time>
                <div className="min-w-0">
                  <strong className="block truncate text-sm">{event.summary}</strong>
                  {event.location && <p className="mt-1 truncate text-xs text-muted-foreground">{event.location}</p>}
                </div>
              </article>
            ))}
            {events.isPending && <p>{copy.loading}</p>}
            {events.error && <p className="text-sm text-destructive">{events.error.message}</p>}
          </div>
        </section>
        <form
          className="grid content-start gap-3 border-t pt-6 lg:border-t-0 lg:border-l lg:pt-0 lg:pl-8"
          onSubmit={(event) => {
            event.preventDefault();
            create.mutate();
          }}
        >
          <h2 className="font-semibold">{copy.newEvent}</h2>
          <Label className="grid gap-1.5">
            {copy.subject}
            <Input value={form.summary} onChange={(event) => setForm({ ...form, summary: event.target.value })} required />
          </Label>
          <Label className="grid gap-1.5">
            {copy.location}
            <Input value={form.location} onChange={(event) => setForm({ ...form, location: event.target.value })} />
          </Label>
          <Label className="grid gap-1.5">
            {copy.start}
            <Input type="datetime-local" value={form.start} onChange={(event) => setForm({ ...form, start: event.target.value })} required />
          </Label>
          <Label className="grid gap-1.5">
            {copy.end}
            <Input type="datetime-local" value={form.end} onChange={(event) => setForm({ ...form, end: event.target.value })} required />
          </Label>
          <Button disabled={create.isPending}>{copy.save}</Button>
        </form>
      </div>
    </ScrollArea>
  );
}
