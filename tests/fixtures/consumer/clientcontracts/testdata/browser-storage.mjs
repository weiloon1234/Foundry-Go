/** Browser storage with independently denied getter, reads and writes. */
export function browserStorage(t) {
  const original = Object.getOwnPropertyDescriptor(globalThis, "window");
  const stored = new Map();
  let denied = null;
  let accesses = 0;
  const check = operation => {
    accesses++;
    if (denied === operation) throw new DOMException("private storage detail", "SecurityError");
  };
  const storage = {
    getItem(key) { check("read"); return stored.get(key) ?? null; },
    setItem(key, value) { check("write"); stored.set(key, value); },
  };
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: { get localStorage() { check("getter"); return storage; } },
  });
  t.after(() => {
    if (original === undefined) delete globalThis.window;
    else Object.defineProperty(globalThis, "window", original);
  });
  return { deny: operation => { denied = operation; }, accesses: () => accesses, stored };
}
