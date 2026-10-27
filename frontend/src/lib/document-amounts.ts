// Match the server's decimal multiplication and round each line to cents.
// Avoid binary floating-point errors such as 1 * 1.005 becoming 1.00.
export function calculateLineAmount(qty: string, price: string): string {
  const decimal = (text: string) => {
    const value = text.trim().replaceAll(",", "");
    if (!/^\d+(?:\.\d+)?$/.test(value)) return null;
    const [whole, fraction = ""] = value.split(".");
    return { value: BigInt(whole + fraction), scale: 10n ** BigInt(fraction.length) };
  };
  const q = decimal(qty), p = decimal(price);
  if (!q || !p || q.value === 0n) return "";
  const divisor = q.scale * p.scale;
  const cents = (q.value * p.value * 100n * 2n + divisor) / (divisor * 2n);
  return `${cents / 100n}.${String(cents % 100n).padStart(2, "0")}`;
}
