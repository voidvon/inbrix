import { ElementType, RowFlex, TableBorder, TdBorder, VerticalAlign, ControlType, type IElement } from "@hufe921/canvas-editor";
import calendarDaysIcon from "../../assets/lucide/calendar-days.svg?raw";
import circleDollarSignIcon from "../../assets/lucide/circle-dollar-sign.svg?raw";
import clock3Icon from "../../assets/lucide/clock-3.svg?raw";
import tagIcon from "../../assets/lucide/tag.svg?raw";
import spiraxQuotationReference from "../../assets/spirax-sarco-quotation-reference.html?raw";

const NAVY = "#13284f";
const BLUE = "#174db2";
const LINE = "#d7dfec";
const PALE = "#f2f6fc";
const FONT = "Arial";

export function createSpiraxQuotationCanvasBackground() {
  const productArt = new DOMParser().parseFromString(spiraxQuotationReference, "text/html").querySelector<HTMLImageElement>(".quote-remarks__art")?.src || "";
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="794" height="1123" viewBox="0 0 794 1123">
    <path fill="#13284f" d="M556 0h238v45H514z"/>
    <g fill="#d7deeb"><circle cx="487" cy="23" r="2.5"/><circle cx="497" cy="23" r="2.5"/><circle cx="507" cy="23" r="2.5"/><circle cx="517" cy="23" r="2.5"/><circle cx="527" cy="23" r="2.5"/><circle cx="537" cy="23" r="2.5"/></g>
    <path fill="#e5e8ed" d="M0 1079l18 44H0z"/>
    ${productArt ? `<image href="${productArt}" x="500" y="846" width="260" height="170" opacity=".18" preserveAspectRatio="xMidYMid meet"/>` : ""}
    <path d="M38 1044h718" fill="none" stroke="${BLUE}" stroke-width="2"/>
    <g transform="translate(42 1051)" fill="none" stroke="${BLUE}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
      <circle cx="7" cy="5" r="3"/>
      <path d="M1 15c0-3 2.5-5 6-5s6 2 6 5"/>
    </g>
    <text x="62" y="1064" fill="${NAVY}" font-family="Arial, sans-serif" font-size="10">Prepared by Shane Zhao</text>
    <g fill="#0f2f6e" transform="translate(680 1056) skewX(-28)"><rect width="7" height="24"/><rect x="14" width="7" height="24"/><rect x="28" width="7" height="24"/><rect x="42" width="42" height="24"/></g>
  </svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

type TextStyle = Pick<IElement, "bold" | "color" | "font" | "size" | "rowFlex" | "rowMargin" | "letterSpacing">;

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

function cell(value: IElement[], backgroundColor?: string, borderTypes?: TdBorder[]) {
  return { colspan: 1, rowspan: 1, value, verticalAlign: VerticalAlign.MIDDLE, backgroundColor, borderTypes };
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

function svgDataURL(selector: string, index = 0, color = "#0a3578") {
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
}

function inlineLucideIcon(svg: string, size = 18): IElement[] {
  const value = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  return [{ type: ElementType.IMAGE, value, width: size, height: size }];
}

function iconBadge(index: number): IElement[] {
  const source = new DOMParser().parseFromString(spiraxQuotationReference, "text/html").querySelectorAll(".quote-icon-badge svg")[index];
  if (!source) return [text("■  ", { bold: true, color: BLUE, size: 15 })];
  source.setAttribute("x", "5");
  source.setAttribute("y", "3");
  source.setAttribute("width", "17");
  source.setAttribute("height", "17");
  source.setAttribute("color", "#ffffff");
  source.querySelectorAll("path,circle,rect").forEach((shape) => {
    shape.setAttribute("fill", "none");
    shape.setAttribute("stroke", "#ffffff");
    shape.setAttribute("stroke-width", "1.75");
    shape.setAttribute("stroke-linecap", "round");
    shape.setAttribute("stroke-linejoin", "round");
  });
  const badge = `<svg xmlns="http://www.w3.org/2000/svg" width="30" height="24" viewBox="0 0 30 24"><path fill="${BLUE}" d="M0 0h30l-5 24H0z"/>${new XMLSerializer().serializeToString(source)}</svg>`;
  return [{ type: ElementType.IMAGE, value: `data:image/svg+xml;charset=utf-8,${encodeURIComponent(badge)}`, width: 23, height: 18 }];
}

function sectionTitleCells(title: string, iconIndex: number) {
  return [cell(iconBadge(iconIndex)), cell([text(title, { bold: true, size: 13 })])];
}

export type QuotationItem = {
  model: string;
  picture?: string;
  description: string;
  qty: string;
  price: string;
  amount: string;
};

export type SpiraxQuotationValues = Record<string, string> & {
  items?: QuotationItem[];
};

function productTable(items?: QuotationItem[], values?: SpiraxQuotationValues) {
  const widths = [110, 108, 200, 80, 110, 110];
  const itemRowHeight = 32;
  const centered = { rowFlex: RowFlex.CENTER, bold: true, size: 10, color: "#ffffff" } as const;
  const header = ["MODEL", "PICTURE", "DESCRIPTION", "QTY", "UNIT PRICE", "AMOUNT"].map((value) => cell([text(value, centered)], NAVY));

  const activeItems: QuotationItem[] = (items && items.length > 0)
    ? items
    : values?.items && values.items.length > 0
    ? values.items
    : [
        {
          model: values?.item_model || "",
          picture: values?.item_picture,
          description: values?.item_description || "",
          qty: values?.item_qty || "",
          price: values?.item_price || "",
          amount: values?.item_amount || "",
        },
      ];

  const itemRows = activeItems.map((item, index) => {
    const isSingle = activeItems.length === 1;
    const modelId = isSingle ? "item_model" : `item_model_${index}`;
    const descId = isSingle ? "item_description" : `item_description_${index}`;
    const qtyId = isSingle ? "item_qty" : `item_qty_${index}`;
    const priceId = isSingle ? "item_price" : `item_price_${index}`;
    const amountId = isSingle ? "item_amount" : `item_amount_${index}`;

    const pictureCellContent: IElement[] = item.picture
      ? [{ type: ElementType.IMAGE, value: item.picture, width: 80, height: 60 }]
      : [text(" ", { size: 10 })];

    return {
      height: item.picture ? Math.max(itemRowHeight, 68) : itemRowHeight,
      cells: [
        cell([controlText(modelId, item.model, "Model", { size: 10 })]),
        cell(pictureCellContent),
        cell([controlText(descId, item.description, "Description", { size: 10 })]),
        cell([controlText(qtyId, item.qty, "Quantity", { size: 10 })]),
        cell([controlText(priceId, item.price, "Unit Price", { size: 10 })]),
        cell([controlText(amountId, item.amount, "Amount", { size: 10 })]),
      ],
    };
  });

  const fillerCount = Math.max(0, 6 - activeItems.length);
  const fillerRows = Array.from({ length: fillerCount }, () => ({
    height: itemRowHeight,
    cells: widths.map(() => cell([text(" ", { size: 11 })])),
  }));

  return table(widths, [
    { height: 32, cells: header, repeat: true },
    ...itemRows,
    ...fillerRows,
    {
      height: 32,
      cells: [
        { ...cell([text("TOTAL", { bold: true, size: 11, rowFlex: RowFlex.RIGHT })], PALE), colspan: 5 },
        cell([controlText("total_amount", values?.total_amount || "", "Total Amount", { bold: true, size: 11, rowFlex: RowFlex.RIGHT })], PALE),
      ],
    },
  ]);
}

export function createSpiraxQuotationCanvasDocument(
  date: string,
  number = "[Quote number]",
  initialValues?: SpiraxQuotationValues,
  items?: QuotationItem[]
): IElement[] {
  const activeItems = (items && items.length > 0) ? items : initialValues?.items;
  const itemCount = activeItems && activeItems.length > 0 ? activeItems.length : 1;

  const logo = svgDataURL(".quote-logo svg");
  const leftHero: IElement[] = [
    ...(logo ? [{ type: ElementType.IMAGE, value: logo, width: 170, height: 50 } as IElement] : [text("spirax sarco", { bold: true, color: "#0a3578", size: 27 })]),
    lineBreak({ size: 1, rowMargin: 1 }),
    text("QUOTATION", { size: 33, color: NAVY, letterSpacing: 1 }),
    lineBreak({ size: 1, rowMargin: 1 }),
    text("Spirax Sarco Quotation", { bold: true, size: 31, color: BLUE }),
    lineBreak({ size: 1, rowMargin: 1 }),
    text("━━━━", { bold: true, size: 7, color: BLUE }),
  ];
  const metaRows: Array<[string, string, string]> = [
    ["Quote No.", initialValues?.quote_number || number, "quote_number"],
    ["Issue Date", initialValues?.issue_date || (date ? date.replaceAll("/", "-") : new Date().toISOString().slice(0, 10)), "issue_date"],
    ["Currency", initialValues?.currency || "USD", "currency"],
    ["Validity", initialValues?.validity || "Valid for 30 days", "validity"],
  ];
  const metaIcons = [tagIcon, calendarDaysIcon, circleDollarSignIcon, clock3Icon];

  const hero = table([430, 26, 88, 174], [
    {
      height: 10,
      cells: [
        { ...cell(leftHero, undefined, [TdBorder.RIGHT]), rowspan: 5, verticalAlign: VerticalAlign.TOP },
        cell([text(" ", { size: 1 })]),
        cell([text(" ", { size: 1 })]),
        cell([text(" ", { size: 1 })]),
      ],
    },
    ...metaRows.map(([label, value, conceptId], index) => ({
      height: 35,
      cells: [
        cell(inlineLucideIcon(metaIcons[index])),
        cell([text(label, { bold: true, color: index === 0 ? BLUE : NAVY, size: 12 })]),
        cell([controlText(conceptId, value, label, { bold: true, rowFlex: RowFlex.RIGHT, size: index === 0 ? 10 : 11 })]),
      ],
    })),
  ], { borderType: TableBorder.EMPTY, borderColor: LINE });

  const customer: Array<[string, string, string]> = [
    ["Company", initialValues?.customer_company || "", "customer_company"],
    ["Contact", initialValues?.customer_contact || "", "customer_contact"],
    ["Phone", initialValues?.customer_phone || "", "customer_phone"],
    ["Email", initialValues?.customer_email || "", "customer_email"],
    ["Address", initialValues?.customer_address || "", "customer_address"],
  ];
  const seller: Array<[string, string, string]> = [
    ["Company", initialValues?.seller_company || "SHANGHAI SPIRAXSARCO FLUID EQUIPMENT CO., LTD", "seller_company"],
    ["Contact", initialValues?.seller_contact || "Shane Zhao", "seller_contact"],
    ["Phone", initialValues?.seller_phone || "+86 157 9019 6438/+44 7707 709941", "seller_phone"],
    ["Email", initialValues?.seller_email || "sales@spiraxsteam.com", "seller_email"],
    ["Address", initialValues?.seller_address || "–", "seller_address"],
  ];
  const parties = table([75, 269, 20, 75, 279], [
    {
      height: 28,
      cells: [
        {
          ...cell([
            ...iconBadge(0),
            text("  Customer Information", { bold: true, size: 12 }),
            lineBreak({ size: 1, rowMargin: 0.2 }),
            { type: ElementType.SEPARATOR, value: "", color: BLUE, lineWidth: 2 },
          ]),
          colspan: 2,
        },
        cell([text(" ", { size: 1 })]),
        {
          ...cell([
            ...iconBadge(1),
            text("  Seller Information", { bold: true, size: 12 }),
            lineBreak({ size: 1, rowMargin: 0.2 }),
            { type: ElementType.SEPARATOR, value: "", color: BLUE, lineWidth: 2 },
          ]),
          colspan: 2,
        },
      ],
    },
    ...customer.map(([cLabel, cVal, cConcept], idx) => {
      const [sLabel, sVal, sConcept] = seller[idx] || ["", "", ""];
      return {
        height: 32,
        cells: [
          cell([text(cLabel, { bold: true, size: 11 })]),
          cell([controlText(cConcept, cVal, cLabel, { size: 11 })]),
          cell([text(" ", { size: 1 })]),
          cell([text(sLabel, { bold: true, size: 11 })]),
          cell([controlText(sConcept, sVal, sLabel, { size: 11 })]),
        ],
      };
    }),
  ], { borderType: TableBorder.EMPTY });

  const terms: Array<[string, string, string]> = [
    ["Lead Time", initialValues?.lead_time || "", "lead_time"],
    ["Payment Terms", initialValues?.payment_terms || "", "payment_terms"],
    ["Validity", initialValues?.commercial_validity || "Valid for 30 days", "commercial_validity"],
    ["Notes", initialValues?.notes || "", "notes"],
  ];
  const remarks = initialValues?.remarks || "";
  const bottomRows: Array<{ height: number; cells: ReturnType<typeof cell>[] }> = [
    {
      height: 30,
      cells: [
        { ...cell([...iconBadge(3), text("  Commercial Terms", { bold: true, size: 12 })], undefined, [TdBorder.BOTTOM]), colspan: 2 },
        cell([...iconBadge(4), text("  Remarks", { bold: true, size: 12 })], undefined, [TdBorder.BOTTOM]),
      ],
    },
    {
      height: 32,
      cells: [
        cell([text(terms[0][0], { bold: true, size: 10 })]),
        cell([controlText(terms[0][2], terms[0][1], terms[0][0], { size: 10 })]),
        { ...cell([controlText("remarks", remarks, "Remarks", { size: 10, rowMargin: 0.35 })]), rowspan: 4 },
      ],
    },
    ...terms.slice(1).map(([label, value, conceptId]) => ({
      height: 32,
      cells: [cell([text(label, { bold: true, size: 10 })]), cell([controlText(conceptId, value, label, { size: 10 })])],
    })),
  ];
  const bottom = table([90, 264, 354], bottomRows, { borderType: TableBorder.EMPTY, borderColor: BLUE });

  return [
    hero,
    lineBreak({ size: 1, rowMargin: 1 }),
    parties,
    lineBreak({ size: 2, rowMargin: 1 }),
    table([31, 569, 118], [{
      height: 24,
      cells: [
        ...sectionTitleCells("Quotation Items", 2),
        cell([text(`${itemCount} items`, { size: 10, rowFlex: RowFlex.RIGHT })])
      ]
    }], { borderType: TableBorder.EMPTY }),
    lineBreak({ size: 1, rowMargin: 1 }),
    productTable(activeItems, initialValues),
    lineBreak({ size: 2, rowMargin: 1 }),
    bottom,
  ];
}
