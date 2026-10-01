const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
function dateValid(text: string): boolean {
  if (!/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(text)) return false;
  const year = Number(text.slice(0, 4)), month = Number(text.slice(5, 7)), day = Number(text.slice(8));
  const days = [31, year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return year >= 1 && month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1]!;
}
function timeValid(text: string): boolean { return /^(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](?:\.[0-9]{1,9})?$/.test(text); }
function dateTimeValid(text: string): boolean {
  const match = /^([0-9]{4}-[0-9]{2}-[0-9]{2})T((?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](?:\.[0-9]{1,9})?)(Z|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])$/.exec(text);
  if (!match || !dateValid(match[1]!)) return false;
  const instant = Date.parse(text); return Number.isFinite(instant) && instant >= -62135596800000 && instant < 253402300800000;
}
function decimalValid(text: string): boolean {
  return text.length <= runtimePolicy.decimalDigits + 2 && /^[+-]?[0-9]+(?:\.[0-9]+)?$/.test(text) && text.replace(/[^0-9]/g, "").length <= runtimePolicy.decimalDigits;
}
function base64Valid(text: string): boolean {
  if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(text)) return false;
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
  if (text.endsWith("==")) return (alphabet.indexOf(text[text.length - 3]!) & 15) === 0;
  if (text.endsWith("=")) return (alphabet.indexOf(text[text.length - 2]!) & 3) === 0;
  return true;
}
function formatValid(format: string | undefined, text: string): boolean {
  switch (format) {
    case undefined: case "": return true;
    case "uuid": return uuidPattern.test(text);
    case "decimal": return decimalValid(text);
    case "date": return dateValid(text);
    case "time": return timeValid(text);
    case "date_time": return dateTimeValid(text);
    case "local_date_time": return text.length >= 19 && /[T ]/.test(text[10]!) && dateValid(text.slice(0, 10)) && timeValid(text.slice(11));
    case "base64": return base64Valid(text);
    case "interval": return intervalValid(text);
    default: return false;
  }
}
// Calendar intervals retain separate months/days/microseconds, like temporal.Interval.
function intervalValid(input: string): boolean {
  if (input.length === 0 || textBytes(input) > 256) return false;
  let text = trimGoSpace(input), months = 0n, days = 0n, micros = 0n;
  const add = (text: string, component: number, weight: bigint, fractional = false): boolean => {
    if (!/^[+-]?[0-9]+(?:\.[0-9]+)?$/.test(text)) return false;
    const negative = text.startsWith("-"); text = text.replace(/^[+-]/, "");
    const [whole, fraction] = text.split(".");
    if (fraction !== undefined && (!fractional || fraction.length > 6)) return false;
    let value = BigInt(whole!) * weight + BigInt((fraction ?? "").padEnd(6, "0"));
    if (negative) value = -value;
    if (component === 0) months += value; else if (component === 1) days += value; else micros += value;
    return true;
  };
  const clock = (text: string): boolean => {
    const negative = text.startsWith("-"); const parts = text.replace(/^[+-]/, "").split(":");
    if (parts.length !== 3 || !/^[0-9]+$/.test(parts[0]!) || !/^[0-5][0-9]$/.test(parts[1]!) || !/^[0-5][0-9](?:\.[0-9]{1,6})?$/.test(parts[2]!)) return false;
    const sign = negative ? "-" : "";
    return add(sign + parts[0], 2, 3600000000n) && add(sign + parts[1], 2, 60000000n) && add(sign + parts[2], 2, 1000000n, true);
  };
  let valid = true;
  if (text.startsWith("P")) {
    text = text.slice(1); let time = false, seen = false, last = 0;
    while (text !== "" && valid) {
      if (text.startsWith("T")) { if (time || text.length === 1) return false; time = true; text = text.slice(1); continue; }
      const match = /^([+-]?[0-9]+(?:\.[0-9]+)?)([YMWDHS])/.exec(text); if (!match) return false;
      const units: Record<string, readonly [number, bigint, number, boolean?]> = time ? { H: [2, 3600000000n, 5], M: [2, 60000000n, 6], S: [2, 1000000n, 7, true] } : { Y: [0, 12n, 1], M: [0, 1n, 2], W: [1, 7n, 3], D: [1, 1n, 4] };
      const unit = units[match[2]!]; if (!unit || unit[2] <= last) return false;
      valid = add(match[1]!, unit[0], unit[1], unit[3]); last = unit[2]; seen = true; text = text.slice(match[0].length);
    }
    valid = valid && seen;
  } else if (/[a-z@]/.test(text)) {
    let tokens = text.split(goSpacePattern).filter(Boolean); if (tokens[0] === "@") tokens = tokens.slice(1);
    const negate = tokens.at(-1) === "ago"; if (negate) tokens = tokens.slice(0, -1);
    if (tokens.length === 1 && tokens[0] === "0") tokens = [];
    else if (!tokens.length) return false;
    const units: Record<string, readonly [number, bigint, number, boolean?]> = {
      year: [0, 12n, 1], years: [0, 12n, 1], mon: [0, 1n, 2], mons: [0, 1n, 2], month: [0, 1n, 2], months: [0, 1n, 2],
      day: [1, 1n, 3], days: [1, 1n, 3], hour: [2, 3600000000n, 4], hours: [2, 3600000000n, 4], min: [2, 60000000n, 5], mins: [2, 60000000n, 5], minute: [2, 60000000n, 5], minutes: [2, 60000000n, 5],
      sec: [2, 1000000n, 6, true], secs: [2, 1000000n, 6, true], second: [2, 1000000n, 6, true], seconds: [2, 1000000n, 6, true], microsecond: [2, 1n, 7], microseconds: [2, 1n, 7],
    };
    let last = 0;
    while (tokens.length) {
      if (tokens[0]!.includes(":")) { if (tokens.length !== 1 || last >= 4 || !clock(tokens[0]!)) return false; break; }
      const unit = units[tokens[1]!]; if (!unit || unit[2] <= last || !add(tokens[0]!, unit[0], unit[1], unit[3])) return false;
      last = unit[2]; tokens = tokens.slice(2);
    }
    if (negate) { months = -months; days = -days; micros = -micros; }
  } else {
    let tokens = text.split(goSpacePattern).filter(Boolean); if (!tokens.length || tokens.length > 3) return false;
    if (tokens.length === 1 && tokens[0] === "0") tokens = [];
    const negative = !!tokens[0]?.startsWith("-") && !tokens.slice(1).some(token => /^[+-]/.test(token));
    if (negative) tokens[0] = tokens[0]!.slice(1);
    const yearMonth = /^([+-]?)([0-9]+)-([0-9]{1,2})$/.exec(tokens[0] ?? "");
    if (yearMonth) { if (Number(yearMonth[3]) > 11 || !add(yearMonth[1]! + yearMonth[2]!, 0, 12n) || !add(yearMonth[1]! + yearMonth[3]!, 0, 1n)) return false; tokens = tokens.slice(1); }
    if (tokens.length && !tokens[0]!.includes(":")) { if (tokens.length !== 2 || !add(tokens[0]!, 1, 1n)) return false; tokens = tokens.slice(1); }
    if (tokens.length && (tokens.length !== 1 || !clock(tokens[0]!))) return false;
    if (negative) { months = -months; days = -days; micros = -micros; }
  }
  return valid && months >= -2147483648n && months <= 2147483647n && days >= -2147483648n && days <= 2147483647n && micros >= -9223372036854775n && micros <= 9223372036854775n;
}
const goSpacePattern = /[\t-\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+/;
function trimGoSpace(text: string): string { return text.replace(/^[\t-\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\t-\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/g, ""); }
export { uuidPattern };
