import { ElementType, RowFlex, TableBorder, TdBorder, VerticalAlign, ControlType, type IElement } from "@hufe921/canvas-editor";
import calendarDaysIcon from "../../assets/lucide/calendar-days.svg?raw";
import circleDollarSignIcon from "../../assets/lucide/circle-dollar-sign.svg?raw";
import clock3Icon from "../../assets/lucide/clock-3.svg?raw";
import tagIcon from "../../assets/lucide/tag.svg?raw";
import spiraxQuotationReference from "../../assets/spirax-sarco-quotation-reference.html?raw";

const BLUE = "#005691";
const NAVY = "#13284f";
const LINE = "#d7dfec";
const PALE = "#f8fafc";
const HIGHLIGHT_BG = "#fef08a";
const HIGHLIGHT_TEXT = "#854d0e";
const FONT = "Arial";

export function createSpiraxContractCanvasBackground(): string {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="794" height="1123" viewBox="0 0 794 1123">
    <!-- Top-right geometric brand accents in Spirax Blue & Navy -->
    <path fill="${BLUE}" d="M520 0h274v42H480z"/>
    <path fill="${NAVY}" d="M670 0h124v42H630z" opacity=".75"/>
    <g fill="#d7deeb">
      <circle cx="445" cy="21" r="2.5"/>
      <circle cx="455" cy="21" r="2.5"/>
      <circle cx="465" cy="21" r="2.5"/>
    </g>
    <!-- Bottom-left fold accent -->
    <path fill="#e2e8f0" d="M0 1080l20 43H0z"/>
    <!-- Bottom footer accent line -->
    <path d="M38 1052h718" fill="none" stroke="${BLUE}" stroke-width="1.5"/>
    <!-- Bottom footer subtle corporate notice -->
    <text x="38" y="1069" fill="#64748b" font-family="Arial, sans-serif" font-size="9">SHANGHAI SPIRAXSARCO FLUID EQUIPMENT CO., LTD.  ·  PROFORMA INVOICE / SALES CONTRACT</text>
    <text x="756" y="1069" text-anchor="end" fill="#94a3b8" font-family="Arial, sans-serif" font-size="8.5">Page 1</text>
    <!-- Bottom-right geometric slashes -->
    <g fill="${BLUE}" transform="translate(685 1076) skewX(-28)">
      <rect width="6" height="20"/>
      <rect x="11" width="6" height="20"/>
      <rect x="22" width="28" height="20"/>
    </g>
  </svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

type TextStyle = Pick<IElement, "bold" | "color" | "font" | "size" | "rowFlex" | "rowMargin" | "letterSpacing" | "italic">;

function text(value: string, style: TextStyle = {}): IElement {
  return { value, font: FONT, color: NAVY, size: 11, ...style };
}

export function controlText(conceptId: string, defaultValue: string, placeholder?: string, style: TextStyle = {}): IElement {
  return {
    type: ElementType.CONTROL,
    value: "",
    font: FONT,
    color: NAVY,
    size: 11,
    ...style,
    control: {
      type: ControlType.TEXT,
      conceptId,
      placeholder: placeholder || defaultValue,
      value: defaultValue ? [text(defaultValue, style)] : null,
    },
  };
}

function lineBreak(style: TextStyle = {}): IElement {
  return text("\n", style);
}

function cell(value: IElement[], backgroundColor?: string, borderTypes?: TdBorder[], verticalAlign = VerticalAlign.MIDDLE) {
  return { colspan: 1, rowspan: 1, value, verticalAlign, backgroundColor, borderTypes };
}

function table(widths: number[], rows: Array<{ height: number; cells: ReturnType<typeof cell>[]; repeat?: boolean }>, options: Partial<IElement> = {}): IElement {
  return {
    type: ElementType.TABLE,
    value: "",
    colgroup: widths.map((width) => ({ width })),
    trList: rows.map((row) => ({ height: row.height, minHeight: row.height, pagingRepeat: row.repeat, tdList: row.cells })),
    borderType: TableBorder.ALL,
    borderColor: LINE,
    borderWidth: 1,
    ...options,
  };
}

function svgDataURL(selector: string, index = 0, color = BLUE) {
  try {
    const source = new DOMParser().parseFromString(spiraxQuotationReference, "text/html").querySelectorAll(selector)[index];
    if (!source) return "";
    source.setAttribute("color", color);
    if (selector.includes("icon")) source.querySelectorAll("path,circle,rect").forEach((shape) => {
      shape.setAttribute("fill", "none");
      shape.setAttribute("stroke", color);
      shape.setAttribute("stroke-width", "1.75");
      shape.setAttribute("stroke-linecap", "round");
      shape.setAttribute("stroke-linejoin", "round");
    });
    return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(new XMLSerializer().serializeToString(source))}`;
  } catch {
    return "";
  }
}

function makeBadge(innerSvg: string, color = BLUE): IElement[] {
  const badge = `<svg xmlns="http://www.w3.org/2000/svg" width="22" height="18" viewBox="0 0 22 18"><rect width="22" height="18" rx="3" fill="${color}"/><g transform="translate(3, 1)" fill="none" stroke="#ffffff" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">${innerSvg}</g></svg>`;
  return [{ type: ElementType.IMAGE, value: `data:image/svg+xml;charset=utf-8,${encodeURIComponent(badge)}`, width: 20, height: 16 }];
}

function inlineLucideIcon(svg: string, size = 18): IElement[] {
  const value = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  return [{ type: ElementType.IMAGE, value, width: size, height: size }];
}

export type ContractItem = {
  model: string;
  description: string;
  qty: string;
  price: string;
  amount: string;
};

export type SpiraxContractValues = Record<string, string> & {
  items?: ContractItem[];
};

export function extractLegacySpiraxContractValues(main: Array<ReturnType<import("@hufe921/canvas-editor").default["command"]["getValue"]>["data"]["main"][number]>): SpiraxContractValues {
  const values: SpiraxContractValues = {};
  const visit = (elements: typeof main) => {
    for (const el of elements) {
      if (el.type === ElementType.CONTROL && el.control?.conceptId) {
        let textVal = "";
        if (el.control.value && Array.isArray(el.control.value)) {
          textVal = el.control.value.map((v) => v.value || "").join("").trim();
        }
        if (textVal) {
          values[el.control.conceptId] = textVal;
        }
      }
      if (el.type === ElementType.TABLE && el.trList) {
        for (const tr of el.trList) {
          if (!tr.tdList) continue;
          for (const td of tr.tdList) {
            if (td.value) visit(td.value);
          }
        }
      }
      if (el.valueList) {
        visit(el.valueList);
      }
    }
  };
  visit(main);

  const extractedItems: ContractItem[] = [];
  let index = 0;
  while (`item_desc_${index}` in values || `item_model_${index}` in values) {
    extractedItems.push({
      model: values[`item_model_${index}`] || "",
      description: values[`item_desc_${index}`] || values[`item_description_${index}`] || "",
      qty: values[`item_qty_${index}`] || "",
      price: values[`item_price_${index}`] || "",
      amount: values[`item_amount_${index}`] || "",
    });
    index++;
  }
  if (extractedItems.length > 0) {
    values.items = extractedItems;
  } else if (values.item_description || values.item_model) {
    values.items = [
      {
        model: values.item_model || "",
        description: values.item_description || "",
        qty: values.item_qty || "",
        price: values.item_price || "",
        amount: values.item_amount || "",
      },
    ];
  }

  return values;
}

function contractItemsTable(items?: ContractItem[], values?: SpiraxContractValues) {
  const widths = [40, 358, 70, 125, 125];
  const itemRowHeight = 58;

  const header = [
    cell([text("Item", { bold: true, color: "#ffffff", size: 11, rowFlex: RowFlex.CENTER })], BLUE),
    cell([text("Description & Specification", { bold: true, color: "#ffffff", size: 11 })], BLUE),
    cell([text("Qty", { bold: true, color: "#ffffff", size: 11, rowFlex: RowFlex.CENTER })], BLUE),
    cell([text("Unit Price (USD)", { bold: true, color: "#ffffff", size: 11, rowFlex: RowFlex.RIGHT })], BLUE),
    cell([text("Amount (USD)", { bold: true, color: "#ffffff", size: 11, rowFlex: RowFlex.RIGHT })], BLUE),
  ];

  const activeItems: ContractItem[] = (items && items.length > 0)
    ? items
    : values?.items && values.items.length > 0
    ? values.items
    : [
        {
          model: values?.item_model || "APT14HC",
          description: values?.item_description || "Spirax Sarco Automatic Pump Trap\nModel: APT14HC\nAutomatic Pump Trap for Condensate Recovery\nBrand: Spirax Sarco (100% Brand New & Original)",
          qty: values?.item_qty || "1 PC",
          price: values?.item_price || "$5,500.00",
          amount: values?.item_amount || "$5,500.00",
        },
      ];

  const itemRows = activeItems.map((item, index) => {
    const isSingle = activeItems.length === 1;
    const descId = isSingle ? "item_description" : `item_desc_${index}`;
    const qtyId = isSingle ? "item_qty" : `item_qty_${index}`;
    const priceId = isSingle ? "item_price" : `item_price_${index}`;
    const amountId = isSingle ? "item_amount" : `item_amount_${index}`;

    const descValue = item.description || (item.model ? `Spirax Sarco Steam Equipment\nModel: ${item.model}` : "Spirax Sarco Steam Equipment");

    return {
      height: itemRowHeight,
      cells: [
        cell([text(String(index + 1), { bold: true, size: 11, rowFlex: RowFlex.CENTER })]),
        cell([controlText(descId, descValue, "Description & Specification", { size: 10.5, rowMargin: 0.28 })]),
        cell([controlText(qtyId, item.qty || "1 PC", "Qty", { size: 11, rowFlex: RowFlex.CENTER })]),
        cell([controlText(priceId, item.price || "$0.00", "Unit Price", { size: 11, rowFlex: RowFlex.RIGHT })]),
        cell([controlText(amountId, item.amount || "$0.00", "Amount", { bold: true, size: 11.5, rowFlex: RowFlex.RIGHT })]),
      ],
    };
  });

  const subtotalVal = values?.subtotal || (activeItems.length === 1 ? activeItems[0].amount : "$5,500.00");
  const freightVal = values?.inland_freight || "FREE";
  const totalVal = values?.total_amount || (activeItems.length === 1 ? `USD ${activeItems[0].amount.replace(/^\$/, "")}` : "USD 5,500.00");
  const sayTotalVal = values?.say_total || "Say Total: US Dollars Five Thousand Five Hundred Only.";

  return table(widths, [
    { height: 32, cells: header, repeat: true },
    ...itemRows,
    {
      height: 29,
      cells: [
        { ...cell([text("Subtotal:", { bold: true, size: 11, color: "#64748b", rowFlex: RowFlex.RIGHT })], undefined, [TdBorder.TOP, TdBorder.BOTTOM]), colspan: 4 },
        cell([controlText("subtotal", subtotalVal, "Subtotal", { bold: true, size: 11.5, rowFlex: RowFlex.RIGHT })], undefined, [TdBorder.TOP, TdBorder.BOTTOM]),
      ],
    },
    {
      height: 29,
      cells: [
        { ...cell([text("Inland Freight (to forwarder):", { bold: true, size: 11, color: "#64748b", rowFlex: RowFlex.RIGHT })], undefined, [TdBorder.BOTTOM]), colspan: 4 },
        cell([controlText("inland_freight", freightVal, "Freight", { bold: true, size: 11.5, color: "#16a34a", rowFlex: RowFlex.RIGHT })], undefined, [TdBorder.BOTTOM]),
      ],
    },
    {
      height: 35,
      cells: [
        { ...cell([text("Total Amount:", { bold: true, size: 12.5, color: BLUE, rowFlex: RowFlex.RIGHT })], "#f1f5f9", [TdBorder.TOP, TdBorder.BOTTOM]), colspan: 4 },
        cell([controlText("total_amount", totalVal, "Total Amount", { bold: true, size: 12.5, color: BLUE, rowFlex: RowFlex.RIGHT })], "#f1f5f9", [TdBorder.TOP, TdBorder.BOTTOM]),
      ],
    },
    {
      height: 29,
      cells: [
        { ...cell([controlText("say_total", sayTotalVal, "Say Total in words", { italic: true, size: 10.5, color: "#475569", rowFlex: RowFlex.RIGHT })], undefined, [TdBorder.TOP, TdBorder.BOTTOM]), colspan: 5 },
      ],
    },
  ]);
}

export function createSpiraxContractCanvasDocument(
  date: string,
  number = "[Contract number]",
  initialValues?: SpiraxContractValues,
  items?: ContractItem[]
): IElement[] {
  const logo = svgDataURL(".quote-logo svg");
  const now = new Date();
  const todayFormatted = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
  const effectiveDate = initialValues?.contract_date || (date && date !== "[Effective date]" ? date.replaceAll("/", "-") : todayFormatted);
  const sellerDate = initialValues?.seller_sign_date || effectiveDate;
  const contractNo = initialValues?.contract_number || number;

  // Left Hero (Seller Brand & Identity)
  const leftHero: IElement[] = [
    ...(logo ? [{ type: ElementType.IMAGE, value: logo, width: 175, height: 50 } as IElement] : [text("SPIRAX SARCO", { bold: true, color: BLUE, size: 22, letterSpacing: 0.8 })]),
    lineBreak({ size: 1, rowMargin: 0.35 }),
    text("First for Steam Solutions", { italic: true, size: 10.5, color: "#64748b" }),
    lineBreak({ size: 1, rowMargin: 0.35 }),
    text("SHANGHAI SPIRAXSARCO FLUID EQUIPMENT CO., LTD.", { bold: true, size: 11, color: "#1e293b" }),
    lineBreak({ size: 1, rowMargin: 0.35 }),
    text("Tel: +86 157 9019 6438 / +1 781 334 8391", { size: 10, color: "#475569" }),
    lineBreak({ size: 1, rowMargin: 0.35 }),
    text("Email: sales@spiraxsteam.com", { size: 10, color: "#475569" }),
  ];

  // Right Meta (Contract / PI details)
  const metaRows: Array<[string, string, string]> = [
    ["PI Number:", contractNo, "contract_number"],
    ["Date:", effectiveDate, "contract_date"],
    ["Customer PO:", initialValues?.customer_po || "OTRP-2026-2366", "customer_po"],
    ["Payment Terms:", initialValues?.payment_terms || "100% T/T Advance", "payment_terms"],
  ];
  const metaIcons = [tagIcon, calendarDaysIcon, circleDollarSignIcon, clock3Icon];

  const metaTable = table([24, 110, 174], [
    ...metaRows.map(([label, value, conceptId], idx) => ({
      height: 27,
      cells: [
        cell(inlineLucideIcon(metaIcons[idx], 18)),
        cell([text(label, { bold: true, color: idx === 0 ? BLUE : NAVY, size: 10.5 })]),
        cell([controlText(conceptId, value, label, { bold: idx === 0, rowFlex: RowFlex.RIGHT, size: idx === 0 ? 11.5 : 11, color: idx === 0 ? BLUE : NAVY })]),
      ],
    })),
  ], { borderType: TableBorder.EMPTY });

  const rightHero: IElement[] = [
    controlText("contract_title", initialValues?.contract_title || "PROFORMA INVOICE", "Document Title", { bold: true, size: 22, color: BLUE, rowFlex: RowFlex.RIGHT }),
    lineBreak({ size: 1, rowMargin: 0.4 }),
    metaTable,
  ];

  const hero = table([410, 308], [{
    height: 148,
    cells: [
      { ...cell(leftHero, undefined, [TdBorder.RIGHT]), verticalAlign: VerticalAlign.TOP },
      { ...cell(rightHero), verticalAlign: VerticalAlign.TOP },
    ],
  }], { borderType: TableBorder.EMPTY, borderColor: LINE });

  // Parties Panels (Bill To Buyer & Delivery Terms)
  const buyerUserBadge = makeBadge('<path d="M12 14c0-2.2-1.8-4-4-4s-4 1.8-4 4"/><circle cx="8" cy="5" r="3"/>');
  const deliveryBadge = makeBadge('<rect x="1" y="3" width="14" height="10" rx="1"/><path d="M1 7h14"/><path d="M5 3v10"/>');

  const buyerRows: Array<[string, string, string, boolean]> = [
    ["Company:", initialValues?.buyer_company || initialValues?.customer_company || "ORA Trading Co.", "buyer_company", true],
    ["Attn:", initialValues?.buyer_contact || initialValues?.customer_contact || "Engr. Muhammad Daood", "buyer_contact", false],
    ["Email:", initialValues?.buyer_email || initialValues?.customer_email || "info@oratrading.com.sa", "buyer_email", false],
    ["Address:", initialValues?.buyer_address || initialValues?.customer_address || "Saudi Arabia", "buyer_address", false],
  ];

  const deliveryRows = [
    ["Trade Term:", initialValues?.trade_term || "FCA China (Free delivery to forwarder warehouse)", "trade_term"],
    ["Lead Time:", initialValues?.lead_time || "Approx. 2 Weeks upon receipt of payment", "lead_time"],
    ["Origin:", initialValues?.origin || "China", "origin"],
    ["Port of Loading:", initialValues?.port_of_loading || "Shanghai, China", "port_of_loading"],
  ];

  const buyerTable = table([74, 270], buyerRows.map(([label, value, conceptId, isBold]) => ({
    height: 27,
    cells: [
      cell([text(label, { bold: true, size: 10.5, color: "#475569" })]),
      cell([controlText(conceptId, value, label, { bold: isBold, size: isBold ? 11.5 : 10.5, color: "#0f172a" })]),
    ],
  })), { borderType: TableBorder.EMPTY });

  const deliveryTable = table([96, 248], deliveryRows.map(([label, value, conceptId]) => ({
    height: 27,
    cells: [
      cell([text(label, { bold: true, size: 10.5, color: "#475569" })]),
      cell([controlText(conceptId, value, label, { size: 10.5, color: "#0f172a" })]),
    ],
  })), { borderType: TableBorder.EMPTY });

  const buyerPanel: IElement[] = [
    table([26, 318], [{
      height: 24,
      cells: [cell(buyerUserBadge), cell([text("Bill To / Buyer Information", { bold: true, size: 11.5, color: BLUE })])],
    }], { borderType: TableBorder.EMPTY }),
    { type: ElementType.SEPARATOR, value: "", color: BLUE, lineWidth: 1.5 },
    lineBreak({ size: 1, rowMargin: 0.25 }),
    buyerTable,
  ];

  const deliveryPanel: IElement[] = [
    table([26, 318], [{
      height: 24,
      cells: [cell(deliveryBadge), cell([text("Delivery & Shipment Terms", { bold: true, size: 11.5, color: BLUE })])],
    }], { borderType: TableBorder.EMPTY }),
    { type: ElementType.SEPARATOR, value: "", color: BLUE, lineWidth: 1.5 },
    lineBreak({ size: 1, rowMargin: 0.25 }),
    deliveryTable,
  ];

  const parties = table([354, 10, 354], [{
    height: 145,
    cells: [
      { ...cell(buyerPanel, PALE), verticalAlign: VerticalAlign.TOP },
      cell([text(" ", { size: 1 })], undefined, []),
      { ...cell(deliveryPanel, PALE), verticalAlign: VerticalAlign.TOP },
    ],
  }], { borderType: TableBorder.EMPTY });

  // Bank details & Order Notes
  const bankBadge = makeBadge('<rect x="1" y="2" width="14" height="11" rx="1.5"/><path d="M1 6h14"/><circle cx="4" cy="10" r="1"/>');
  const notesBadge = makeBadge('<path d="M3 2h8l3 3v8a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1z"/><path d="M10 2v4h4"/>');

  const buyerCompany = initialValues?.buyer_company || initialValues?.customer_company || "ORA Trading Co.";
  const productModel = initialValues?.item_model || (items && items[0]?.model) || "APT14HC";
  const defaultMemo = `${buyerCompany} / ${contractNo} / ${productModel}`;
  const paymentMemo = initialValues?.payment_memo
    ? initialValues.payment_memo
        .replace(/\bPI-20260919-01\b/g, contractNo)
        .replace(/\[(?:Contract number|合同编号)\]/gi, contractNo)
    : defaultMemo;

  const bankRows: Array<[string, string, string, boolean]> = [
    ["Beneficiary Name:", initialValues?.bank_account_name || "SHANGHAI SPIRAXSARCO FLUID EQUIPMENT CO., LTD", "bank_account_name", true],
    ["Account Number:", initialValues?.bank_account_no || "LU024080000056414710", "bank_account_no", true],
    ["SWIFT / BIC Code:", initialValues?.bank_swift || "BCIRLULL (or BCIRLULLXXX)", "bank_swift", true],
    ["Bank Name:", initialValues?.bank_name || "Banking Circle S.A.", "bank_name", false],
    ["Bank Address:", initialValues?.bank_address || "2 Boulevard de la Foire L-1528 Luxembourg", "bank_address", false],
    ["Country / Region:", initialValues?.bank_country || "Luxembourg", "bank_country", false],
    ["Account Type:", initialValues?.bank_account_type || "Business Account", "bank_account_type", false],
    ["Payment Method:", initialValues?.payment_method || "SWIFT (Wire/TT) · Multi-Currency (USD, EUR, SAR)", "payment_method", false],
    ["Payment Memo / Note:", paymentMemo, "payment_memo", true],
  ];

  const bankInnerTable = table([136, 290], bankRows.map(([label, value, conceptId, isHighlight]) => ({
    height: 24,
    cells: [
      cell([text(label, { size: 9.5, color: "#64748b", bold: true })], PALE),
      cell(
        [
          controlText(conceptId, value, label, {
            size: isHighlight ? 9.5 : 10,
            bold: isHighlight || conceptId === "bank_account_no" || conceptId === "bank_swift",
            color: isHighlight ? HIGHLIGHT_TEXT : "#0f172a",
          }),
        ],
        isHighlight ? HIGHLIGHT_BG : undefined
      ),
    ],
  })), { borderType: TableBorder.ALL, borderColor: LINE });

  const bankPanel: IElement[] = [
    table([26, 400], [{
      height: 24,
      cells: [cell(bankBadge), cell([text("Official Bank Payment Details (SWIFT / Wire TT)", { bold: true, size: 11, color: BLUE })])],
    }], { borderType: TableBorder.EMPTY }),
    { type: ElementType.SEPARATOR, value: "", color: BLUE, lineWidth: 1.5 },
    lineBreak({ size: 1, rowMargin: 0.25 }),
    bankInnerTable,
  ];

  const notesPanel: IElement[] = [
    table([26, 252], [{
      height: 24,
      cells: [cell(notesBadge), cell([text("Order Notes & Terms", { bold: true, size: 11, color: BLUE })])],
    }], { borderType: TableBorder.EMPTY }),
    { type: ElementType.SEPARATOR, value: "", color: BLUE, lineWidth: 1.5 },
    lineBreak({ size: 1, rowMargin: 0.25 }),
    text("1. Production & Dispatch Schedule:", { bold: true, size: 10, color: "#1e293b" }),
    lineBreak({ size: 1, rowMargin: 0.1 }),
    controlText("order_note_1", initialValues?.order_note_1 || "Goods prepared within approx. 2 weeks upon receiving bank confirmation.", "Note 1", { size: 9.5, color: "#475569" }),
    lineBreak({ size: 1, rowMargin: 0.25 }),
    text("2. Bank Remittance Slip (MT103):", { bold: true, size: 10, color: "#1e293b" }),
    lineBreak({ size: 1, rowMargin: 0.1 }),
    controlText("order_note_2", initialValues?.order_note_2 || "Kindly provide official bank SWIFT MT103 copy once remittance is executed.", "Note 2", { size: 9.5, color: "#475569" }),
    lineBreak({ size: 1, rowMargin: 0.25 }),
    text("3. Delivery & Forwarder Handover:", { bold: true, size: 10, color: "#1e293b" }),
    lineBreak({ size: 1, rowMargin: 0.1 }),
    controlText("order_note_3", initialValues?.order_note_3 || "Commercial invoice & packing list provided ahead of forwarder collection.", "Note 3", { size: 9.5, color: "#475569" }),
    lineBreak({ size: 1, rowMargin: 0.25 }),
    text("4. Quality & Warranty Commitment:", { bold: true, size: 10, color: "#1e293b" }),
    lineBreak({ size: 1, rowMargin: 0.1 }),
    controlText("order_note_4", initialValues?.order_note_4 || "100% Brand New & Genuine Spirax Sarco equipment with standard warranty.", "Note 4", { size: 9.5, color: "#475569" }),
  ];

  const bottomSection = table([426, 10, 282], [{
    height: 255,
    cells: [
      { ...cell(bankPanel), verticalAlign: VerticalAlign.TOP },
      cell([text(" ", { size: 1 })], undefined, []),
      { ...cell(notesPanel, PALE), verticalAlign: VerticalAlign.TOP },
    ],
  }], { borderType: TableBorder.EMPTY });

  // Signature Block
  const signatureSection = table([340, 38, 340], [{
    height: 110,
    cells: [
      {
        ...cell([
          lineBreak({ size: 4, rowMargin: 0.25 }),
          text("Authorized Signature & Corporate Stamp (Seller):", { bold: true, size: 10.5, color: "#475569" }),
          lineBreak({ size: 1, rowMargin: 0.2 }),
          controlText("seller_sign_company", initialValues?.seller_sign_company || "Shanghai Spiraxsarco Fluid Equipment Co., Ltd.", "Seller Company", { bold: true, size: 11, color: "#1e293b" }),
          lineBreak({ size: 1, rowMargin: 2 }),
          text("Date: ", { bold: true, size: 9.5, color: "#475569" }),
          controlText("seller_sign_date", sellerDate, "Date", { bold: true, size: 9.5, color: "#0f172a" }),
          text("       Sign / Stamp: ____________________", { size: 9.5, color: "#64748b" }),
        ], undefined, [TdBorder.TOP]),
        verticalAlign: VerticalAlign.TOP,
      },
      cell([text(" ", { size: 1 })], undefined, []),
      {
        ...cell([
          lineBreak({ size: 4, rowMargin: 0.25 }),
          text("Accepted & Confirmed By (Buyer):", { bold: true, size: 10.5, color: "#475569" }),
          lineBreak({ size: 1, rowMargin: 0.2 }),
          controlText("buyer_sign_name", initialValues?.buyer_sign_name || "Engr. Muhammad Daood / ORA Trading Co.", "Buyer Signature", { bold: true, size: 11, color: "#1e293b" }),
          lineBreak({ size: 1, rowMargin: 2 }),
          text("Date: ____________________       Authorized Sign: ____________________", { size: 9.5, color: "#64748b" }),
        ], undefined, [TdBorder.TOP]),
        verticalAlign: VerticalAlign.TOP,
      },
    ],
  }], { borderType: TableBorder.EMPTY, borderColor: BLUE });

  return [
    hero,
    lineBreak({ size: 2, rowMargin: 0.4 }),
    parties,
    lineBreak({ size: 2, rowMargin: 0.4 }),
    contractItemsTable(items, initialValues),
    lineBreak({ size: 2, rowMargin: 0.4 }),
    bottomSection,
    lineBreak({ size: 10, rowMargin: 0.8 }),
    signatureSection,
  ];
}
