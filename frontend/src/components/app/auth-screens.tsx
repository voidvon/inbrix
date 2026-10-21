import { useState, type ChangeEvent, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Mail } from "lucide-react";
import { toast, toastError } from "../../lib/toast";
import { ApiError, getPublicSettings, register, signIn } from "../../lib/api";
import type { Copy } from "../../lib/locale";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Skeleton } from "../ui/skeleton";

export function LoginScreen({ copy }: { copy: Copy }) {
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [invalid, setInvalid] = useState<{ login?: boolean; password?: boolean }>({});
  const publicSettings = useQuery({ queryKey: ["public-settings"], queryFn: getPublicSettings, retry: false });
  const mutation = useMutation({
    mutationFn: () => signIn(login, password),
    onSuccess: (result) => window.location.assign(result.next),
    onError: (value) => toast.error(value instanceof ApiError && value.status === 401 ? copy.invalidCredentials : copy.loginFailed),
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const missingLogin = !login.trim();
    const missingPassword = !password;
    if (missingLogin || missingPassword) {
      setInvalid({ login: missingLogin, password: missingPassword });
      toast.error(copy.requiredFieldsMissing);
      return;
    }
    setInvalid({});
    mutation.mutate();
  };

  return (
    <main className="grid min-h-screen place-items-center bg-background p-6">
      <div className="grid w-full max-w-sm gap-4 rounded-xl border bg-card p-6 ring-1 ring-foreground/5">
        <div className="flex items-center gap-2 text-lg font-semibold">
          <span className="grid size-8 place-items-center rounded-md bg-primary text-primary-foreground">
            <Mail className="size-4" />
          </span>
          <strong>Inbrix AI</strong>
        </div>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight">{copy.login}</h1>
        <p className="-mt-2 text-sm text-muted-foreground">{copy.appAccount}</p>
        <form
          className="grid gap-4"
          noValidate
          onSubmit={submit}
        >
          <Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="login-account">
            {copy.appAccount}
            <Input id="login-account" value={login} aria-invalid={invalid.login} onChange={(event) => { setLogin(event.target.value); if (invalid.login) setInvalid((current) => ({ ...current, login: false })); }} autoComplete="username" required />
          </Label>
          <Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="login-password">
            {copy.password}
            <Input id="login-password" type="password" value={password} aria-invalid={invalid.password} onChange={(event) => { setPassword(event.target.value); if (invalid.password) setInvalid((current) => ({ ...current, password: false })); }} autoComplete="current-password" required />
          </Label>
          <Button type="submit" className="w-full" disabled={mutation.isPending}>
            {mutation.isPending ? copy.loading : copy.login}
          </Button>
        </form>
        {publicSettings.data?.registrationOpen && (
          <Button nativeButton={false} render={<a href="/register" />} variant="link" size="sm">
            {copy.createAccount}
          </Button>
        )}
      </div>
    </main>
  );
}

export function RegisterScreen({ copy }: { copy: Copy }) {
  const [form, setForm] = useState({ login: "", displayName: "", password: "", confirmation: "" });
  const [invalid, setInvalid] = useState<{ login?: boolean; password?: boolean; confirmation?: boolean }>({});
  const publicSettings = useQuery({ queryKey: ["public-settings"], queryFn: getPublicSettings, retry: false });
  const mutation = useMutation({
    mutationFn: () => register(form.login, form.displayName, form.password, form.confirmation),
    onSuccess: (result) => window.location.assign(result.next),
    onError: (value) => toastError(value, copy.loginFailed),
  });

  const field = (key: keyof typeof form) => (event: ChangeEvent<HTMLInputElement>) => {
    setForm((value) => ({ ...value, [key]: event.target.value }));
    if (key !== "displayName" && invalid[key]) {
      setInvalid((current) => ({ ...current, [key]: false }));
    }
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const missingLogin = !form.login.trim();
    const missingPassword = !form.password;
    const missingConfirmation = !form.confirmation;
    if (missingLogin || missingPassword || missingConfirmation) {
      setInvalid({ login: missingLogin, password: missingPassword, confirmation: missingConfirmation });
      toast.error(copy.requiredFieldsMissing);
      return;
    }
    if (form.password.length < 8) {
      setInvalid({ password: true });
      toast.error(copy.passwordRequirements);
      return;
    }
    if (form.password !== form.confirmation) {
      setInvalid({ password: true, confirmation: true });
      toast.error(copy.passwordMismatch);
      return;
    }
    setInvalid({});
    mutation.mutate();
  };

  return (
    <main className="grid min-h-screen place-items-center bg-background p-6">
      <div className="grid w-full max-w-sm gap-4 rounded-lg border bg-card p-6">
        <div className="flex items-center gap-2 text-lg font-semibold">
          <span className="grid size-8 place-items-center rounded-md bg-primary text-primary-foreground">
            <Mail className="size-4" />
          </span>
          Inbrix AI
        </div>
        <h1 className="text-xl font-semibold">{copy.createAccount}</h1>
        {publicSettings.isPending ? (
          <Skeleton className="h-48 w-full" />
        ) : publicSettings.data?.registrationOpen ? (
          <form
            className="grid gap-3"
            noValidate
            onSubmit={submit}
          >
            <Label className="grid gap-1.5">
              {copy.appAccount}
              <Input value={form.login} aria-invalid={invalid.login} onChange={field("login")} required />
            </Label>
            <Label className="grid gap-1.5">
              {copy.displayName}
              <Input value={form.displayName} onChange={field("displayName")} />
            </Label>
            <Label className="grid gap-1.5">
              {copy.password}
              <Input type="password" minLength={8} value={form.password} aria-invalid={invalid.password} onChange={field("password")} required />
            </Label>
            <Label className="grid gap-1.5">
              {copy.confirmNewPassword}
              <Input type="password" minLength={8} value={form.confirmation} aria-invalid={invalid.confirmation} onChange={field("confirmation")} required />
            </Label>
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? copy.loading : copy.createAccount}
            </Button>
          </form>
        ) : (
          <p className="py-6 text-sm text-muted-foreground">{copy.registrationClosed}</p>
        )}
        <Button nativeButton={false} render={<a href="/login" />} variant="link">
          {copy.login}
        </Button>
      </div>
    </main>
  );
}
