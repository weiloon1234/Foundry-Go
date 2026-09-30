function metadataLimits(): JSONLimits { return { Bytes: runtimePolicy.metadataBytes, Depth: runtimePolicy.maxDepth, Nodes: 1 << 20, Steps: 1 << 22, Issues: 1 }; }
/** Returns an owned lossless snapshot, including enum labels, permissions, locales and tables. */
export function contractMetadata(): JSONValue { return parseWire(manifestJSON, metadataLimits()); }
function runtimeMetadata(value: JSONValue, bounded = false, clampLimits = true): unknown {
  if (value instanceof JSONNumber) {
    const number = Number(value.text);
    if (Number.isSafeInteger(number) && integerPattern.test(value.text)) return number;
    if (clampLimits && bounded && number >= 0) return Math.min(number, Number.MAX_SAFE_INTEGER);
    return value;
  }
  if (Array.isArray(value)) return value.map(item => runtimeMetadata(item, bounded, clampLimits));
  if (value !== null && typeof value === "object") {
    const result: Record<string, unknown> = Object.create(null);
    for (const [key, item] of Object.entries(value)) result[key] = runtimeMetadata(item as JSONValue, bounded || key === "limits" || key === "file_transfer_bytes", clampLimits);
    return result;
  }
  return value;
}
// clampLimits=false keeps unsafe-size limits exact for public inspection; the
// invoker's operational copy clamps them.
function loadRuntimeDocument(clampLimits = true): RuntimeDocument {
  const document = runtimeMetadata(contractMetadata(), false, clampLimits) as RuntimeDocument;
  if (document.version !== manifestVersion) reject("", "manifest_version");
  return document;
}
