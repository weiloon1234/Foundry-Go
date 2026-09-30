import { useSyncExternalStore } from "react";
import type { FormStore } from "./contracts_foundry.gen.js";

/** Borrow a form or task. The screen/request owner disposes it, not this hook.
 * Create a component-owned controller in the effect that disposes it, not during
 * render, so StrictMode replaces it rather than reusing a disposed one.
 * SSR needs the same initial snapshot on the server and during hydration.
 */
export function useForm<S>(store: FormStore<S>, getServerSnapshot: () => S = store.getSnapshot): S {
  return useSyncExternalStore(store.subscribe, store.getSnapshot, getServerSnapshot);
}
