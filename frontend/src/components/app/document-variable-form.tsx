import { useEffect, useMemo, useState } from "react";
import {
  Building2,
  Calendar,
  Check,
  CircleDollarSign,
  FileSpreadsheet,
  FileText,
  Landmark,
  Package,
  Plus,
  RotateCcw,
  Search,
  Sparkles,
  Trash2,
  Truck,
  User,
} from "lucide-react";
import { toast } from "sonner";
import type { CanvasDocumentEditorHandle, DocumentDataPayload } from "./canvas-document-editor";
import type { QuotationItem } from "./spirax-quotation-canvas";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Textarea } from "../ui/textarea";
import { Badge } from "../ui/badge";
import { Separator } from "../ui/separator";
import { type Copy, zh } from "../../lib/locale";
import { cn } from "../../lib/utils";

// Human-friendly labels for known conceptIds
const VARIABLE_LABELS: Record<string, { zh: string; en: string; group: "basic" | "customer" | "seller" | "terms" | "bank" }> = {
  // Basic
  quote_number: { zh: "报价单号", en: "Quote Number", group: "basic" },
  contract_number: { zh: "合同编号 (PI Number)", en: "Contract / PI Number", group: "basic" },
  issue_date: { zh: "报价日期", en: "Issue Date", group: "basic" },
  contract_date: { zh: "合同日期", en: "Contract Date", group: "basic" },
  currency: { zh: "结算币种", en: "Currency", group: "basic" },
  validity: { zh: "报价有效期", en: "Validity", group: "basic" },
  commercial_validity: { zh: "商务有效期", en: "Commercial Validity", group: "basic" },
  contract_title: { zh: "文档标题", en: "Document Title", group: "basic" },
  customer_po: { zh: "客户 PO 编号", en: "Customer PO Number", group: "basic" },

  // Customer / Buyer
  customer_company: { zh: "客户/买方公司", en: "Customer / Buyer Company", group: "customer" },
  buyer_company: { zh: "买方公司名称", en: "Buyer Company", group: "customer" },
  customer_contact: { zh: "客户联系人", en: "Contact Person", group: "customer" },
  buyer_contact: { zh: "买方联系人", en: "Buyer Contact", group: "customer" },
  customer_phone: { zh: "客户电话", en: "Phone Number", group: "customer" },
  customer_email: { zh: "客户邮箱", en: "Email Address", group: "customer" },
  buyer_email: { zh: "买方邮箱", en: "Buyer Email", group: "customer" },
  customer_address: { zh: "客户地址", en: "Customer Address", group: "customer" },
  buyer_address: { zh: "买方地址", en: "Buyer Address", group: "customer" },

  // Seller
  seller_company: { zh: "销售方公司", en: "Seller Company", group: "seller" },
  seller_sign_company: { zh: "签署方公司 (卖方)", en: "Seller Sign Company", group: "seller" },
  seller_contact: { zh: "销售联系人", en: "Seller Contact", group: "seller" },
  seller_phone: { zh: "销售电话", en: "Seller Phone", group: "seller" },
  seller_email: { zh: "销售邮箱", en: "Seller Email", group: "seller" },
  seller_address: { zh: "销售地址", en: "Seller Address", group: "seller" },
  seller_sign_date: { zh: "卖方签署日期", en: "Seller Sign Date", group: "seller" },

  // Delivery & Terms
  trade_term: { zh: "贸易条款 (如 FOB)", en: "Trade Terms (e.g. FOB)", group: "terms" },
  lead_time: { zh: "交货期 / 货期", en: "Lead Time", group: "terms" },
  origin: { zh: "原产地", en: "Country of Origin", group: "terms" },
  port_of_loading: { zh: "装运港口", en: "Port of Loading", group: "terms" },
  payment_terms: { zh: "付款方式与条款", en: "Payment Terms", group: "terms" },
  remarks: { zh: "特别备注", en: "Remarks", group: "terms" },
  notes: { zh: "补充说明", en: "Notes", group: "terms" },
  payment_memo: { zh: "付款备注标识", en: "Payment Memo", group: "terms" },
  total_amount: { zh: "总计金额", en: "Total Amount", group: "terms" },
  subtotal: { zh: "小计金额", en: "Subtotal", group: "terms" },
  inland_freight: { zh: "内陆运费", en: "Inland Freight", group: "terms" },
  say_total: { zh: "大写总额", en: "Say Total in Words", group: "terms" },
  order_note_1: { zh: "条款 1: 排产交期", en: "Clause 1: Production Schedule", group: "terms" },
  order_note_2: { zh: "条款 2: 水单要求", en: "Clause 2: Bank Remittance", group: "terms" },
  order_note_3: { zh: "条款 3: 货代交接", en: "Clause 3: Forwarder Handover", group: "terms" },
  order_note_4: { zh: "条款 4: 质保承诺", en: "Clause 4: Warranty Commitment", group: "terms" },

  // Bank
  bank_account_name: { zh: "收款人户名", en: "Beneficiary Name", group: "bank" },
  bank_account_no: { zh: "银行账号 (IBAN)", en: "Account Number", group: "bank" },
  bank_swift: { zh: "SWIFT / BIC 代码", en: "SWIFT / BIC Code", group: "bank" },
  bank_name: { zh: "开户银行名称", en: "Bank Name", group: "bank" },
  bank_address: { zh: "银行详细地址", en: "Bank Address", group: "bank" },
  bank_country: { zh: "银行所在国家", en: "Bank Country", group: "bank" },
  bank_account_type: { zh: "账户类型", en: "Account Type", group: "bank" },
  payment_method: { zh: "支持汇款方式", en: "Payment Method", group: "bank" },
};

export function DocumentVariableForm({
  copy,
  editor,
  onApplyComplete,
  aiSidebarOpen = true,
  onToggleAi,
}: {
  copy: Copy;
  editor: CanvasDocumentEditorHandle | null;
  onApplyComplete?: (switchToDocument: boolean) => void;
  aiSidebarOpen?: boolean;
  onToggleAi?: () => void;
}) {
  const isZh = copy === zh;
  const [data, setData] = useState<DocumentDataPayload | null>(null);
  const [values, setValues] = useState<Record<string, string>>({});
  const [items, setItems] = useState<QuotationItem[]>([]);
  const [query, setQuery] = useState("");
  const [activeGroup, setActiveGroup] = useState<string>("all");

  const loadFromDocument = () => {
    if (!editor) return;
    try {
      const docData = editor.getDocumentData();
      setData(docData);
      setValues({ ...docData.values });
      setItems(docData.items ? docData.items.map((it) => ({ ...it })) : []);
    } catch (err) {
      console.error("Failed to load variables from document", err);
    }
  };

  useEffect(() => {
    loadFromDocument();
  }, [editor]);

  const handleValueChange = (key: string, val: string) => {
    setValues((prev) => ({ ...prev, [key]: val }));
  };

  const handleItemChange = (index: number, field: keyof QuotationItem, val: string) => {
    setItems((prev) => {
      const next = [...prev];
      const cur = { ...next[index], [field]: val };

      // Auto-compute amount if qty & price are updated
      if (field === "qty" || field === "price") {
        const q = parseFloat(cur.qty.replace(/[^0-9.]/g, ""));
        const p = parseFloat(cur.price.replace(/[^0-9.]/g, ""));
        if (!isNaN(q) && !isNaN(p)) {
          cur.amount = (q * p).toFixed(2);
        }
      }
      next[index] = cur;

      // Auto calculate total
      const total = next.reduce((sum, it) => {
        const amt = parseFloat(it.amount.replace(/[^0-9.]/g, ""));
        return sum + (isNaN(amt) ? 0 : amt);
      }, 0);
      if (total > 0) {
        setValues((v) => ({ ...v, total_amount: total.toFixed(2), subtotal: total.toFixed(2) }));
      }

      return next;
    });
  };

  const handleAddItem = () => {
    setItems((prev) => [
      ...prev,
      {
        model: "",
        description: "",
        qty: "1",
        price: "",
        amount: "",
      },
    ]);
  };

  const handleRemoveItem = (index: number) => {
    setItems((prev) => prev.filter((_, i) => i !== index));
  };

  const handleApply = (switchToDocument: boolean) => {
    if (!editor) return;
    try {
      editor.applyDocumentUpdates({
        values,
        items: items.length > 0 ? items : undefined,
      });
      toast.success(isZh ? "变量已成功填充到文档！" : "Variables applied to document!");
      onApplyComplete?.(switchToDocument);
    } catch (err) {
      console.error("Failed to apply variables", err);
      toast.error(isZh ? "填充失败，请检查填写内容" : "Failed to apply variables");
    }
  };

  // Group the variables
  const groupedVariables = useMemo(() => {
    const basic: Array<{ key: string; label: string; value: string }> = [];
    const customer: Array<{ key: string; label: string; value: string }> = [];
    const seller: Array<{ key: string; label: string; value: string }> = [];
    const terms: Array<{ key: string; label: string; value: string }> = [];
    const bank: Array<{ key: string; label: string; value: string }> = [];
    const other: Array<{ key: string; label: string; value: string }> = [];

    // All known variables or keys currently in values
    const allKeys = new Set([...Object.keys(values)]);

    // Also include template-typical keys even if currently empty
    const templateKind = data?.template || "document";
    if (templateKind === "spirax-quotation") {
      ["quote_number", "issue_date", "currency", "validity", "customer_company", "customer_contact", "customer_phone", "customer_email", "customer_address", "seller_company", "seller_contact", "seller_phone", "seller_email", "seller_address", "lead_time", "payment_terms", "commercial_validity", "notes", "remarks"].forEach((k) => allKeys.add(k));
    } else if (templateKind === "spirax-contract") {
      ["contract_number", "contract_date", "contract_title", "customer_po", "buyer_company", "buyer_contact", "buyer_email", "buyer_address", "trade_term", "lead_time", "origin", "port_of_loading", "payment_terms", "bank_account_name", "bank_account_no", "bank_swift", "bank_name", "bank_address", "bank_country", "payment_memo", "order_note_1", "order_note_2", "order_note_3", "order_note_4"].forEach((k) => allKeys.add(k));
    }

    for (const key of allKeys) {
      if (key.startsWith("item_")) continue; // Handled in items table
      const meta = VARIABLE_LABELS[key];
      const label = meta ? (isZh ? meta.zh : meta.en) : key.replace(/^[\[_]+|[\]_]+$/g, "");
      const val = values[key] ?? "";

      if (query.trim()) {
        const q = query.trim().toLowerCase();
        if (!key.toLowerCase().includes(q) && !label.toLowerCase().includes(q) && !val.toLowerCase().includes(q)) {
          continue;
        }
      }

      const item = { key, label, value: val };
      if (!meta) other.push(item);
      else if (meta.group === "basic") basic.push(item);
      else if (meta.group === "customer") customer.push(item);
      else if (meta.group === "seller") seller.push(item);
      else if (meta.group === "terms") terms.push(item);
      else if (meta.group === "bank") bank.push(item);
    }

    return { basic, customer, seller, terms, bank, other };
  }, [values, query, isZh, data?.template]);

  const totalVarCount = Object.keys(values).filter((k) => !k.startsWith("item_")).length + items.length;

  return (
    <div className="flex h-full w-full flex-col bg-background overflow-hidden">
      {/* Action Sub-header */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b bg-muted/25 px-4 py-2.5 sm:px-6">
        <div className="flex items-center gap-2">
          <Badge variant="secondary" className="gap-1 font-normal text-xs">
            <Sparkles className="size-3 text-primary" />
            {isZh ? `已识别 ${totalVarCount} 项文档变量` : `${totalVarCount} variables ready`}
          </Badge>
          <div className="relative w-44 sm:w-60">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={isZh ? "搜索变量名称或内容…" : "Search variables…"}
              className="h-7 pl-8 text-xs bg-background"
            />
          </div>
        </div>
        <div className="flex items-center gap-2">
          {onToggleAi && !aiSidebarOpen && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 text-xs text-primary border-primary/30 hover:bg-primary/5 shadow-2xs"
              onClick={onToggleAi}
            >
              <Sparkles className="size-3.5 text-primary" />
              <span>{isZh ? "AI 文档助手" : "AI Assistant"}</span>
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-8 gap-1.5 text-xs"
            onClick={loadFromDocument}
            title={isZh ? "放弃当前修改，从文档同步" : "Reload from document"}
          >
            <RotateCcw className="size-3.5" />
            {isZh ? "重新读取" : "Reload"}
          </Button>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            className="h-8 gap-1.5 text-xs"
            onClick={() => handleApply(false)}
          >
            <Check className="size-3.5 text-primary" />
            {isZh ? "仅填充数据" : "Apply Values"}
          </Button>
          <Button
            type="button"
            size="sm"
            className="h-8 gap-1.5 text-xs"
            onClick={() => handleApply(true)}
          >
            <FileText className="size-3.5" />
            {isZh ? "填充并查看文档" : "Apply & View Document"}
          </Button>
        </div>
      </div>

      {/* Main Content Area */}
      <div className="flex-1 overflow-y-auto p-4 sm:p-6 space-y-6 max-w-5xl mx-auto w-full">
        {/* Category Filter Pills */}
        <div className="flex flex-wrap gap-1.5 pb-1">
          {[
            { id: "all", label: isZh ? "全部变量" : "All" },
            { id: "basic", label: isZh ? "基础信息" : "Basic Meta", icon: Calendar },
            { id: "customer", label: isZh ? "购买方 / 客户" : "Customer / Buyer", icon: Building2 },
            { id: "seller", label: isZh ? "销售方信息" : "Seller", icon: User },
            { id: "items", label: isZh ? "产品列表" : "Products", icon: Package },
            { id: "terms", label: isZh ? "商务条款与备注" : "Terms & Notes", icon: Truck },
            ...(groupedVariables.bank.length > 0 ? [{ id: "bank", label: isZh ? "银行账户" : "Bank Info", icon: Landmark }] : []),
          ].map((cat) => {
            const Icon = cat.icon;
            return (
              <button
                key={cat.id}
                type="button"
                onClick={() => setActiveGroup(cat.id)}
                className={cn(
                  "inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium transition-colors",
                  activeGroup === cat.id
                    ? "bg-primary text-primary-foreground shadow-sm"
                    : "bg-muted/60 text-muted-foreground hover:bg-muted hover:text-foreground"
                )}
              >
                {Icon && <Icon className="size-3" />}
                {cat.label}
              </button>
            );
          })}
        </div>

        {/* 1. Basic Info Section */}
        {(activeGroup === "all" || activeGroup === "basic") && groupedVariables.basic.length > 0 && (
          <section className="rounded-xl border bg-card p-4 sm:p-5 shadow-sm space-y-4">
            <div className="flex items-center gap-2 border-b pb-2.5">
              <Calendar className="size-4 text-primary" />
              <h3 className="text-sm font-semibold text-foreground">{isZh ? "单据基本信息" : "Document Information"}</h3>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
              {groupedVariables.basic.map((item) => (
                <div key={item.key} className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <Label htmlFor={item.key} className="text-xs font-medium text-foreground">
                      {item.label}
                    </Label>
                    <span className="text-[10px] text-muted-foreground font-mono">{item.key}</span>
                  </div>
                  <Input
                    id={item.key}
                    value={item.value}
                    onChange={(e) => handleValueChange(item.key, e.target.value)}
                    placeholder={item.label}
                    className="h-8 text-xs"
                  />
                </div>
              ))}
            </div>
          </section>
        )}

        {/* 2. Customer / Buyer Section */}
        {(activeGroup === "all" || activeGroup === "customer") && groupedVariables.customer.length > 0 && (
          <section className="rounded-xl border bg-card p-4 sm:p-5 shadow-sm space-y-4">
            <div className="flex items-center gap-2 border-b pb-2.5">
              <Building2 className="size-4 text-primary" />
              <h3 className="text-sm font-semibold text-foreground">{isZh ? "购买方 / 客户信息" : "Buyer & Customer Details"}</h3>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              {groupedVariables.customer.map((item) => (
                <div key={item.key} className={cn("space-y-1.5", (item.key.includes("address") || item.key.includes("company")) && "sm:col-span-2")}>
                  <div className="flex items-center justify-between">
                    <Label htmlFor={item.key} className="text-xs font-medium text-foreground">
                      {item.label}
                    </Label>
                    <span className="text-[10px] text-muted-foreground font-mono">{item.key}</span>
                  </div>
                  {item.key.includes("address") ? (
                    <Textarea
                      id={item.key}
                      value={item.value}
                      onChange={(e) => handleValueChange(item.key, e.target.value)}
                      placeholder={item.label}
                      rows={2}
                      className="text-xs resize-none"
                    />
                  ) : (
                    <Input
                      id={item.key}
                      value={item.value}
                      onChange={(e) => handleValueChange(item.key, e.target.value)}
                      placeholder={item.label}
                      className="h-8 text-xs"
                    />
                  )}
                </div>
              ))}
            </div>
          </section>
        )}

        {/* 3. Seller Section */}
        {(activeGroup === "all" || activeGroup === "seller") && groupedVariables.seller.length > 0 && (
          <section className="rounded-xl border bg-card p-4 sm:p-5 shadow-sm space-y-4">
            <div className="flex items-center gap-2 border-b pb-2.5">
              <User className="size-4 text-primary" />
              <h3 className="text-sm font-semibold text-foreground">{isZh ? "销售方信息" : "Seller Details"}</h3>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              {groupedVariables.seller.map((item) => (
                <div key={item.key} className={cn("space-y-1.5", (item.key.includes("address") || item.key.includes("company")) && "sm:col-span-2")}>
                  <div className="flex items-center justify-between">
                    <Label htmlFor={item.key} className="text-xs font-medium text-foreground">
                      {item.label}
                    </Label>
                    <span className="text-[10px] text-muted-foreground font-mono">{item.key}</span>
                  </div>
                  {item.key.includes("address") ? (
                    <Textarea
                      id={item.key}
                      value={item.value}
                      onChange={(e) => handleValueChange(item.key, e.target.value)}
                      placeholder={item.label}
                      rows={2}
                      className="text-xs resize-none"
                    />
                  ) : (
                    <Input
                      id={item.key}
                      value={item.value}
                      onChange={(e) => handleValueChange(item.key, e.target.value)}
                      placeholder={item.label}
                      className="h-8 text-xs"
                    />
                  )}
                </div>
              ))}
            </div>
          </section>
        )}

        {/* 4. Products / Items Table Section */}
        {(activeGroup === "all" || activeGroup === "items") && (
          <section className="rounded-xl border bg-card p-4 sm:p-5 shadow-sm space-y-4">
            <div className="flex items-center justify-between border-b pb-2.5">
              <div className="flex items-center gap-2">
                <Package className="size-4 text-primary" />
                <h3 className="text-sm font-semibold text-foreground">{isZh ? "产品明细清单" : "Quotation & Contract Items"}</h3>
                <Badge variant="outline" className="text-[10px]">{items.length} {isZh ? "项" : "items"}</Badge>
              </div>
              <Button type="button" variant="outline" size="sm" onClick={handleAddItem} className="h-7 gap-1 text-xs">
                <Plus className="size-3.5" />
                {isZh ? "添加产品行" : "Add Item"}
              </Button>
            </div>

            {items.length === 0 ? (
              <div className="py-8 text-center text-xs text-muted-foreground border border-dashed rounded-lg">
                <p>{isZh ? "暂无产品明细，点击上方按钮添加产品行" : "No items listed. Click 'Add Item' above."}</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs border-collapse">
                  <thead>
                    <tr className="border-b bg-muted/40 text-muted-foreground font-medium">
                      <th className="py-2 px-2.5 w-10 text-center">#</th>
                      <th className="py-2 px-2.5 w-36">{isZh ? "产品型号 (Model)" : "Model"}</th>
                      <th className="py-2 px-2.5 min-w-48">{isZh ? "描述 / 规格 (Description)" : "Description"}</th>
                      <th className="py-2 px-2.5 w-20 text-center">{isZh ? "数量" : "Qty"}</th>
                      <th className="py-2 px-2.5 w-28 text-right">{isZh ? "单价" : "Unit Price"}</th>
                      <th className="py-2 px-2.5 w-28 text-right">{isZh ? "金额" : "Amount"}</th>
                      <th className="py-2 px-2 w-12 text-center">{isZh ? "操作" : "Action"}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {items.map((item, idx) => (
                      <tr key={idx} className="hover:bg-muted/20 transition-colors">
                        <td className="py-2 px-2 text-center text-muted-foreground font-mono">{idx + 1}</td>
                        <td className="py-2 px-2">
                          <Input
                            value={item.model}
                            onChange={(e) => handleItemChange(idx, "model", e.target.value)}
                            placeholder={isZh ? "产品型号" : "Model"}
                            className="h-7 text-xs"
                          />
                        </td>
                        <td className="py-2 px-2">
                          <Input
                            value={item.description}
                            onChange={(e) => handleItemChange(idx, "description", e.target.value)}
                            placeholder={isZh ? "规格描述" : "Description"}
                            className="h-7 text-xs"
                          />
                        </td>
                        <td className="py-2 px-2">
                          <Input
                            value={item.qty}
                            onChange={(e) => handleItemChange(idx, "qty", e.target.value)}
                            placeholder="1"
                            className="h-7 text-xs text-center"
                          />
                        </td>
                        <td className="py-2 px-2">
                          <Input
                            value={item.price}
                            onChange={(e) => handleItemChange(idx, "price", e.target.value)}
                            placeholder="0.00"
                            className="h-7 text-xs text-right"
                          />
                        </td>
                        <td className="py-2 px-2">
                          <Input
                            value={item.amount}
                            onChange={(e) => handleItemChange(idx, "amount", e.target.value)}
                            placeholder="0.00"
                            className="h-7 text-xs text-right font-medium"
                          />
                        </td>
                        <td className="py-2 px-2 text-center">
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="size-7 text-muted-foreground hover:text-destructive"
                            onClick={() => handleRemoveItem(idx)}
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        )}

        {/* 5. Delivery & Terms Section */}
        {(activeGroup === "all" || activeGroup === "terms") && groupedVariables.terms.length > 0 && (
          <section className="rounded-xl border bg-card p-4 sm:p-5 shadow-sm space-y-4">
            <div className="flex items-center gap-2 border-b pb-2.5">
              <Truck className="size-4 text-primary" />
              <h3 className="text-sm font-semibold text-foreground">{isZh ? "商务条款、交期与特别备注" : "Commercial Terms & Delivery"}</h3>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              {groupedVariables.terms.map((item) => (
                <div key={item.key} className={cn("space-y-1.5", (item.key.includes("remark") || item.key.includes("note")) && "sm:col-span-2")}>
                  <div className="flex items-center justify-between">
                    <Label htmlFor={item.key} className="text-xs font-medium text-foreground">
                      {item.label}
                    </Label>
                    <span className="text-[10px] text-muted-foreground font-mono">{item.key}</span>
                  </div>
                  {item.key.includes("remark") || item.key.includes("note") ? (
                    <Textarea
                      id={item.key}
                      value={item.value}
                      onChange={(e) => handleValueChange(item.key, e.target.value)}
                      placeholder={item.label}
                      rows={2}
                      className="text-xs resize-none"
                    />
                  ) : (
                    <Input
                      id={item.key}
                      value={item.value}
                      onChange={(e) => handleValueChange(item.key, e.target.value)}
                      placeholder={item.label}
                      className="h-8 text-xs"
                    />
                  )}
                </div>
              ))}
            </div>
          </section>
        )}

        {/* 6. Bank Details Section (Contract) */}
        {(activeGroup === "all" || activeGroup === "bank") && groupedVariables.bank.length > 0 && (
          <section className="rounded-xl border bg-card p-4 sm:p-5 shadow-sm space-y-4">
            <div className="flex items-center gap-2 border-b pb-2.5">
              <Landmark className="size-4 text-primary" />
              <h3 className="text-sm font-semibold text-foreground">{isZh ? "官方银行收款信息 (SWIFT / Wire TT)" : "Bank Details"}</h3>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
              {groupedVariables.bank.map((item) => (
                <div key={item.key} className={cn("space-y-1.5", item.key.includes("address") && "sm:col-span-2 md:col-span-3")}>
                  <div className="flex items-center justify-between">
                    <Label htmlFor={item.key} className="text-xs font-medium text-foreground">
                      {item.label}
                    </Label>
                    <span className="text-[10px] text-muted-foreground font-mono">{item.key}</span>
                  </div>
                  <Input
                    id={item.key}
                    value={item.value}
                    onChange={(e) => handleValueChange(item.key, e.target.value)}
                    placeholder={item.label}
                    className="h-8 text-xs"
                  />
                </div>
              ))}
            </div>
          </section>
        )}

        {/* 7. Other Custom / Placeholder Variables */}
        {groupedVariables.other.length > 0 && (
          <section className="rounded-xl border bg-card p-4 sm:p-5 shadow-sm space-y-4">
            <div className="flex items-center gap-2 border-b pb-2.5">
              <FileSpreadsheet className="size-4 text-primary" />
              <h3 className="text-sm font-semibold text-foreground">{isZh ? "其他自定义占位符与变量" : "Custom Variables & Placeholders"}</h3>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
              {groupedVariables.other.map((item) => (
                <div key={item.key} className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <Label htmlFor={item.key} className="text-xs font-medium text-foreground">
                      {item.label}
                    </Label>
                    <span className="text-[10px] text-muted-foreground font-mono">{item.key}</span>
                  </div>
                  <Input
                    id={item.key}
                    value={item.value}
                    onChange={(e) => handleValueChange(item.key, e.target.value)}
                    placeholder={item.label}
                    className="h-8 text-xs"
                  />
                </div>
              ))}
            </div>
          </section>
        )}
      </div>

      {/* Sticky Bottom Summary / Action Bar */}
      <div className="flex items-center justify-between border-t bg-card px-4 py-3 sm:px-6 shadow-md">
        <div className="text-xs text-muted-foreground">
          {isZh ? "在表单中填写修改变量，点击右侧按钮即可实时写入文档排版中。" : "Edit variables here and apply them to update the document layout."}
        </div>
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => handleApply(false)}
            className="text-xs"
          >
            {isZh ? "仅填充数据" : "Apply Values"}
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={() => handleApply(true)}
            className="text-xs gap-1.5"
          >
            <Check className="size-3.5" />
            {isZh ? "填充并查看文档" : "Apply & View Document"}
          </Button>
        </div>
      </div>
    </div>
  );
}
