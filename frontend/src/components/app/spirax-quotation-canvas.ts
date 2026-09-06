import { ElementType, RowFlex, TableBorder, TdBorder, VerticalAlign, type IElement } from "@hufe921/canvas-editor";
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
  return [{ type: ElementType.IMAGE, value: `data:image/svg+xml;charset=utf-8,${encodeURIComponent(badge)}`, width: 23, height: 18 }, text("  ", { size: 3 })];
}

function sectionTitle(title: string, iconIndex: number, trailing = ""): IElement[] {
  return [
    ...iconBadge(iconIndex),
    text(title, { bold: true, size: 13 }),
    ...(trailing ? [text(trailing, { size: 10, rowFlex: RowFlex.RIGHT })] : []),
    lineBreak({ size: 1, rowMargin: 1 }),
  ];
}

function infoTable(rows: Array<[string, string]>, width = 345) {
  return table([70, width - 70], rows.map(([label, value]) => ({
    height: 13,
    cells: [
      cell([text(label, { bold: true, size: 11 })]),
      cell([text(value, { size: 11 })]),
    ],
  })), { borderType: TableBorder.EMPTY });
}

function partyPanel(title: string, rows: Array<[string, string]>, width: number, iconIndex: number): IElement[] {
  return [
    ...sectionTitle(title, iconIndex),
    {
      type: ElementType.SEPARATOR,
      value: "",
      color: BLUE,
      lineWidth: 2,
    },
    lineBreak({ size: 1, rowMargin: 1 }),
    infoTable(rows, width),
  ];
}

function productTable() {
  const widths = [136, 222, 115, 122, 123];
  const centered = { rowFlex: RowFlex.CENTER, bold: true, size: 10, color: "#ffffff" } as const;
  const header = ["MODEL", "DESCRIPTION", "QTY", "UNIT PRICE", "AMOUNT"].map((value) => cell([text(value, centered)], NAVY));
  const firstRow = ["SP400", "Spirax Sarco SP400", "1", "1,634.47", "1,634.47"].map((value) => cell([text(value, { size: 11 })]));
  const fillerRows = Array.from({ length: 6 }, () => ({ height: 25, cells: widths.map(() => cell([text(" ", { size: 11 })])) }));
  return table(widths, [
    { height: 36, cells: header, repeat: true },
    { height: 38, cells: firstRow },
    ...fillerRows,
    {
      height: 38,
      cells: [
        { ...cell([text("TOTAL", { bold: true, size: 11, rowFlex: RowFlex.RIGHT })], PALE), colspan: 4 },
        cell([text("1,634.47", { bold: true, size: 11, rowFlex: RowFlex.RIGHT })], PALE),
      ],
    },
  ]);
}

export function createSpiraxQuotationCanvasDocument(date: string): IElement[] {
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
  const metaRows: Array<[string, string]> = [
    ["Quote No.", `SP-${new Date().getFullYear()}081410`],
    ["Issue Date", date.replaceAll("/", "-")],
    ["Currency", "USD"],
    ["Validity", "Valid for 30 days"],
  ];
  const metaIcons = [tagIcon, calendarDaysIcon, circleDollarSignIcon, clock3Icon];
  const meta = table([26, 89, 135], [
    { height: 10, cells: [cell([text(" ", { size: 1 })]), cell([text(" ", { size: 1 })]), cell([text(" ", { size: 1 })])] },
    ...metaRows.map(([label, value], index) => ({
      height: 30,
      cells: [
        cell(inlineLucideIcon(metaIcons[index])),
        cell([text(label, { bold: true, color: index === 0 ? BLUE : NAVY, size: 12 })]),
        cell([text(value, { bold: true, rowFlex: RowFlex.RIGHT, size: 11 })]),
      ],
    })),
  ], { borderType: TableBorder.EMPTY });
  const hero = table([430, 288], [{
    height: 178,
    cells: [cell(leftHero, undefined, [TdBorder.RIGHT]), { ...cell([meta]), verticalAlign: VerticalAlign.TOP }],
  }], { borderType: TableBorder.EMPTY, borderColor: LINE });

  const customer: Array<[string, string]> = [
    ["Company", 'LLC "Bocco"'],
    ["Contact", "Mariia Savostian, Manager of Supply and Foreign Economic Activity"],
    ["Phone", "+38 067 826 09 10 (Viber, Telegram, WhatsApp)"],
    ["Email", "–"],
    ["Address", "25006 Kropyvnytskyi, Ukraine"],
  ];
  const seller: Array<[string, string]> = [
    ["Company", "SHANGHAI SPIRAXSARCO FLUID EQUIPMENT CO., LTD"],
    ["Contact", "Shane Zhao"],
    ["Phone", "+86 157 9019 6438/+44 7707 709941"],
    ["Email", "sales@spiraxsteam.com"],
    ["Address", "–"],
  ];
  const parties = table([354, 354], [{
    height: 112,
    cells: [cell(partyPanel("Customer Information", customer, 340, 0)), cell(partyPanel("Seller Information", seller, 340, 1))],
  }], { borderType: TableBorder.EMPTY });

  const terms: Array<[string, string]> = [
    ["Lead Time", "In stock; available for prompt shipment."],
    ["Payment Terms", "30% advance payment, 70% before shipment"],
    ["Validity", "Valid for 30 days"],
    ["Notes", "Price converted from CNY 11,000 at an exchange rate of 1 USD = 6.73 CNY."],
  ];
  const remarks = "Price converted from CNY 11,000 at an exchange rate of 1 USD = 6.73 CNY. Shipping cost to 25006 Kropyvnytskyi, Ukraine is not included and will be quoted separately.";
  const bottomRows: Array<{ height: number; cells: ReturnType<typeof cell>[] }> = [
    {
      height: 30,
      cells: [
        { ...cell([...iconBadge(3), text("Commercial Terms", { bold: true, size: 13 })], undefined, [TdBorder.BOTTOM]), colspan: 2 },
        cell([...iconBadge(4), text("Remarks", { bold: true, size: 13 })], undefined, [TdBorder.BOTTOM]),
      ],
    },
    {
      height: 20,
      cells: [
        cell([text(terms[0][0], { bold: true, size: 10 })]),
        cell([text(terms[0][1], { size: 10 })]),
        { ...cell([text(remarks, { size: 10, rowMargin: 1.45 })]), rowspan: 4 },
      ],
    },
    ...terms.slice(1).map(([label, value]) => ({
      height: 20,
      cells: [cell([text(label, { bold: true, size: 10 })]), cell([text(value, { size: 10 })])],
    })),
  ];
  const bottom = table([90, 264, 354], bottomRows, { borderType: TableBorder.EMPTY, borderColor: BLUE });

  return [
    hero,
    lineBreak({ size: 1, rowMargin: 1 }),
    parties,
    lineBreak({ size: 2, rowMargin: 1 }),
    table([600, 118], [{ height: 24, cells: [cell([...iconBadge(2), text("Quotation Items", { bold: true, size: 13 })]), cell([text("1 items", { size: 10, rowFlex: RowFlex.RIGHT })])] }], { borderType: TableBorder.EMPTY }),
    lineBreak({ size: 1, rowMargin: 1 }),
    productTable(),
    lineBreak({ size: 2, rowMargin: 1 }),
    bottom,
  ];
}
