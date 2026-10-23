import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { emptyAccountBrands, getAccountBrands, getAccounts, saveAccountBrands, type AccountBrands, type AIBusinessContext, type AIBusinessProfile } from "../../lib/api";
import { toast } from "../../lib/toast";
import { type Copy, zh } from "../../lib/locale";
import { listProductNavigation } from "../../lib/products";
import { fetchServerTemplates } from "../../lib/document-storage";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "../ui/dropdown-menu";
import { ProductSelect } from "./product-select";

const tasks = [
  { value: "", zh: "自动选择任务", en: "Auto task" },
  { value: "email_analysis", zh: "邮件需求分析", en: "Email analysis" },
  { value: "quotation", zh: "产品选型与报价", en: "Product selection & quotation" },
  { value: "contract", zh: "销售合同制作", en: "Sales contract" },
];

function BrandPicker({ brands, available, onChange, disabled, label, resetLabel, emptyLabel }: { brands: string[]; available: string[]; onChange: (brands: string[]) => void; disabled?: boolean; label: string; resetLabel: string; emptyLabel: string }) {
  return <DropdownMenu><DropdownMenuTrigger render={<Button type="button" variant="outline" size="sm" disabled={disabled} className="max-w-full gap-1 text-xs" aria-label={label}><span className="truncate">{label}</span><ChevronDown className="size-3 shrink-0" /></Button>} />
    <DropdownMenuContent className="max-h-72 max-w-80 overflow-y-auto">
      <DropdownMenuItem onClick={() => onChange([])}>{resetLabel}</DropdownMenuItem>
      {!available.length && !brands.length && <DropdownMenuItem disabled>{emptyLabel}</DropdownMenuItem>}
      {Array.from(new Set([...available, ...brands])).map(brand => <DropdownMenuCheckboxItem key={brand} checked={brands.includes(brand)} onCheckedChange={checked => onChange(checked ? [...brands, brand] : brands.filter(b => b !== brand))}>{brand}</DropdownMenuCheckboxItem>)}
    </DropdownMenuContent>
  </DropdownMenu>;
}

export function AIBusinessProfileFields({ copy, value, onChange, disabled }: { copy: Copy; value: AIBusinessProfile; onChange: (value: AIBusinessProfile) => void; disabled: boolean }) {
  const isZh = copy === zh;
  const templates = useQuery({ queryKey: ["document-templates", isZh], queryFn: () => fetchServerTemplates(copy) });
  const patch = (next: Partial<AIBusinessProfile>) => onChange({ ...value, ...next });
  return <div className="grid gap-4 rounded-lg border p-3">
    <p className="text-sm font-medium">{isZh ? "业务助手配置" : "Business assistant"}</p>
    <div className="grid gap-2"><Label htmlFor="assistant-company">{isZh ? "卖方公司" : "Seller company"}</Label><Input id="assistant-company" value={value.company} onChange={e => patch({ company: e.target.value })} disabled={disabled} /></div>
    {(["quotation", "contract"] as const).map(type => {
      const key = type === "quotation" ? "quotationTemplateId" : "contractTemplateId";
      return <div className="grid gap-2" key={type}><Label>{isZh ? `默认${type === "quotation" ? "报价单" : "合同"}模板` : `Default ${type} template`}</Label><ProductSelect value={value[key]} label={key} disabled={disabled || templates.isPending} onValueChange={id => patch({ [key]: id })} options={[{ value: "", label: isZh ? "系统默认模板" : "System default" }, ...(templates.data || []).filter(t => t.type === type).map(t => ({ value: t.id, label: t.name }))]} /></div>;
    })}
    <p className="text-xs text-muted-foreground">{isZh ? "内置 Skill：邮件需求分析 → 产品选型与报价 → 销售合同制作。各品牌共用任务流程，价格以产品库或已约定的报价为准。" : "Built-in skills: email analysis → selection & quotation → sales contract. Brands share workflows; prices come from the catalog or an agreed quotation."}</p>
  </div>;
}

export function AIBusinessToolbar({ copy, accountEmail, value, onChange, disabled }: { copy: Copy; accountEmail?: string; value: AIBusinessContext; onChange: (value: AIBusinessContext) => void; disabled?: boolean }) {
  const isZh = copy === zh;
  const navigation = useQuery({ queryKey: ["product-navigation"], queryFn: listProductNavigation });
  const binding = useMailboxBrands(accountEmail);
  const profile = binding.data;
  const restricted = profile?.brandMode === "restrict";
  const available = restricted ? profile.brands : (navigation.data?.brands || []).map(b => b.name).filter(Boolean);
  const inherited = profile?.brands || [];
  const label = value.brands.length ? value.brands.join("、") : inherited.length ? `${restricted ? (isZh ? "限定" : "Only") : (isZh ? "优先" : "Prefer")} ${inherited.join("、")}` : (isZh ? "全部品牌" : "All brands");
  return <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
    <div className="min-w-0 max-w-full flex-1"><ProductSelect label={isZh ? "任务 Skill" : "Task skill"} value={value.skillId} disabled={disabled} onValueChange={skillId => onChange({ ...value, skillId })} options={tasks.map(t => ({ value: t.value, label: isZh ? t.zh : t.en }))} className="h-8 text-xs" /></div>
    {binding.isError && <span className="text-xs text-destructive">{isZh ? "邮箱品牌加载失败" : "Mailbox brands unavailable"}</span>}
    <BrandPicker brands={value.brands} available={available} disabled={disabled || binding.isPending || binding.isError} onChange={brands => onChange({ ...value, brands })} resetLabel={isZh ? "使用邮箱默认品牌" : "Use mailbox defaults"} emptyLabel={isZh ? "产品库中暂无品牌" : "No catalog brands"} label={`${isZh ? "品牌" : "Brands"}: ${label}`} />
  </div>;
}

// Both chat surfaces share the mailbox setting cache with the settings dialog.
function useMailboxBrands(email?: string) {
  const accounts = useQuery({ queryKey: ["accounts"], queryFn: getAccounts, enabled: Boolean(email) });
  const id = accounts.data?.accounts.find(a => a.email === email)?.id;
  const binding = useQuery({ queryKey: ["account-brands", id], queryFn: () => getAccountBrands(id!), enabled: Boolean(id) });
  return { data: binding.data, isPending: Boolean(email) && (accounts.isPending || Boolean(id) && binding.isPending), isError: accounts.isError || binding.isError || Boolean(email) && accounts.isSuccess && !id };
}

export function AccountBrandSettings({ copy, accountId }: { copy: Copy; accountId: string }) {
  const isZh = copy === zh;
  const queryClient = useQueryClient();
  const binding = useQuery({ queryKey: ["account-brands", accountId], queryFn: () => getAccountBrands(accountId) });
  const navigation = useQuery({ queryKey: ["product-navigation"], queryFn: listProductNavigation });
  const [draft, setDraft] = useState<AccountBrands | null>(null);
  const value = draft || binding.data || emptyAccountBrands;
  const disabled = binding.isPending || binding.isError;
  const patch = (next: Partial<AccountBrands>) => setDraft({ ...value, ...next });
  const save = useMutation({ mutationFn: () => saveAccountBrands(accountId, value), onSuccess: saved => {
    queryClient.setQueryData(["account-brands", accountId], saved);
    setDraft(null);
    toast.success(isZh ? "邮箱品牌已保存" : "Mailbox brands saved");
  }, onError: (error: Error) => toast.error(error.message) });
  return <div className="grid gap-3 rounded-lg border p-3">
    <div><h3 className="text-sm font-semibold">{isZh ? "邮箱品牌绑定" : "Mailbox brands"}</h3><p className="mt-1 text-xs text-muted-foreground">{isZh ? "此邮箱的邮件分析、报价与合同共用以下品牌，切换智能体不改变品牌范围。保存后立即生效。" : "Email analysis, quotations and contracts use this mailbox’s brands across assistants. Saving takes effect immediately."}</p></div>
    <div className="grid gap-2"><Label>{isZh ? "负责品牌" : "Responsible brands"}</Label><BrandPicker brands={value.brands} available={(navigation.data?.brands || []).map(b => b.name).filter(Boolean)} onChange={brands => patch({ brands })} disabled={disabled || save.isPending} resetLabel={isZh ? "清空已选品牌" : "Clear brands"} emptyLabel={isZh ? "产品库中暂无品牌" : "No catalog brands"} label={value.brands.join("、") || (isZh ? "选择产品库品牌" : "Select catalog brands")} />
      {navigation.isError && <p className="text-xs text-destructive">{isZh ? "品牌加载失败，请重试" : "Failed to load brands"}</p>}
      <ProductSelect label={isZh ? "品牌范围模式" : "Brand mode"} value={value.brandMode} disabled={disabled || save.isPending} onValueChange={brandMode => patch({ brandMode: brandMode as AccountBrands["brandMode"] })} options={[{ value: "prefer", label: isZh ? "优先这些品牌，允许按客户需求查询其他品牌" : "Prefer these brands; allow other requested brands" }, { value: "restrict", label: isZh ? "仅允许这些品牌（产品查询与文档均受限）" : "Restrict catalog and documents to these brands" }]} />
    </div>

    {binding.isError && <p role="alert" className="text-xs text-destructive">{isZh ? "邮箱品牌加载失败" : "Failed to load mailbox brands"}<Button type="button" variant="link" onClick={() => void binding.refetch()}>{isZh ? "重试" : "Retry"}</Button></p>}
    <Button type="button" size="sm" className="justify-self-start" disabled={disabled || save.isPending || !draft || value.brandMode === "restrict" && !value.brands.length} onClick={() => save.mutate()}>{isZh ? "保存品牌绑定" : "Save mailbox brands"}</Button>
  </div>;
}
