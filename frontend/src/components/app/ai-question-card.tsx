import { useId, useState } from "react";
import type { AIUserQuestion } from "../../types";
import { Button } from "../ui/button";
import { Textarea } from "../ui/textarea";

/** A dedicated answer form; the last radio choice always allows a custom answer. */
export function AIQuestionCard({ question, isZh, disabled, onSubmit }: {
  question: AIUserQuestion;
  isZh: boolean;
  disabled: boolean;
  onSubmit: (answer: string) => void;
}) {
  const id = useId();
  const [selected, setSelected] = useState<number | null>(null);
  const [custom, setCustom] = useState("");
  const isCustom = selected === question.options.length;
  const answer = isCustom ? custom.trim() : selected === null ? "" : question.options[selected];
  return (
    <form data-testid="ai-question-card" className="space-y-3 rounded-xl border border-primary/25 bg-primary/5 p-3" onSubmit={event => {
      event.preventDefault();
      if (!disabled && answer) onSubmit(answer);
    }}>
      <fieldset disabled={disabled} className="min-w-0 space-y-2">
        <legend className="mb-2 text-sm font-medium whitespace-pre-wrap">{question.question}</legend>
        <p className="text-xs text-muted-foreground">{isZh ? "请选择一项，或自行填写答案。" : "Choose one option or enter your own answer."}</p>
        {[...question.options, isZh ? "其他，自行输入" : "Other, enter your own answer"].map((option, index) => (
          <label key={index} className="flex cursor-pointer items-start gap-2 rounded-lg border bg-background p-2 text-sm has-checked:border-primary has-checked:bg-primary/5">
            <input type="radio" name={id} value={index} checked={selected === index} onChange={() => setSelected(index)} className="mt-1 accent-primary" />
            <span className="min-w-0 break-words">{option}</span>
          </label>
        ))}
        {isCustom && <Textarea autoFocus value={custom} onChange={event => setCustom(event.target.value)} aria-label={isZh ? "你的答案" : "Your answer"} placeholder={isZh ? "请输入你的答案…" : "Enter your answer…"} maxLength={4000} className="min-h-20 bg-background" />}
      </fieldset>
      <Button type="submit" size="sm" disabled={disabled || !answer}>{isZh ? "提交并继续" : "Submit and continue"}</Button>
    </form>
  );
}
