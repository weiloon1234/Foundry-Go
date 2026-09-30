import { getCurrentScope, onScopeDispose, shallowReadonly, shallowRef } from "vue";
import type { ShallowRef } from "vue";
import type { FormStore } from "./contracts_foundry.gen.js";

/** Borrow a form or task in setup/effectScope. Only the subscription is owned.
 * Keep a separate controller per SSR request; dispose it when the owner exits.
 */
export function useForm<S>(store: FormStore<S>): Readonly<ShallowRef<S>> {
  if (!getCurrentScope()) throw new Error("useForm requires an active Vue scope");
  // Allocate our own ref even when a borrowed store's snapshot is itself a ref.
  const snapshot = shallowRef<S>();
  snapshot.value = store.getSnapshot();
  const stop = store.subscribe(() => { snapshot.value = store.getSnapshot(); });
  onScopeDispose(stop);
  snapshot.value = store.getSnapshot();
  // The outer ref is initialized before exposure and never receives undefined
  // unless undefined is already part of the store's snapshot type.
  return shallowReadonly(snapshot) as Readonly<ShallowRef<S>>;
}
