import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
import { JSDOM } from "jsdom";

const dom = new JSDOM("<!doctype html><html><body></body></html>", { url: "https://forms.test/" });
for (const name of ["window", "document", "navigator", "HTMLElement", "Element", "Node", "SVGElement"]) Object.defineProperty(globalThis, name, { value: name === "window" ? dom.window : dom.window[name], configurable: true });
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const sdkURL = pathToFileURL(process.argv[2]);
const sdk = await import(sdkURL.href);
const React = await import("react");
const { createRoot, hydrateRoot } = await import("react-dom/client");
const { renderToString } = await import("react-dom/server");
const { useForm: useReactForm } = await import(new URL("./contracts_react_foundry.gen.js", sdkURL));
const Vue = await import("vue");
const { useForm: useVueForm } = await import(new URL("./contracts_vue_foundry.gen.js", sdkURL));
const { renderToString: renderVue } = await import("@vue/server-renderer");

function owned(name) {
  const op = sdk.operation("formsSubmit");
  const form = sdk.createForm(op, { body: { name, "tags[]": [] } });
  let subscriptions = 0;
  const store = { getSnapshot: form.getSnapshot, subscribe(listener) { subscriptions++; const stop = form.subscribe(listener); return () => { subscriptions--; stop(); }; } };
  return { form, store, field: form.field(op.field("body", "name")), get subscriptions() { return subscriptions; } };
}
const reactOwner = owned("React initial");
function ReactForm({ owner }) {
  const snapshot = useReactForm(owner.store);
  return React.createElement("output", null, snapshot.text[owner.field.path] ?? snapshot.values.body.name);
}
const host = document.createElement("div"); document.body.append(host);
const root = createRoot(host);
await React.act(async () => { root.render(React.createElement(React.StrictMode, null, React.createElement(ReactForm, { owner: reactOwner }))); });
assert.equal(host.textContent, "React initial"); assert.equal(reactOwner.subscriptions, 1);
await React.act(async () => { reactOwner.field.setText("React draft"); }); assert.equal(host.textContent, "React draft");
await React.act(async () => { reactOwner.field.parse(); }); assert.equal(host.textContent, "React draft");
await React.act(async () => { root.unmount(); }); assert.equal(reactOwner.subscriptions, 0);
reactOwner.field.set("still owned"); reactOwner.form.dispose(); host.remove();

// Real server rendering and hydration preserve independent per-request state.
const server = owned("SSR one"), other = owned("SSR two");
const markup = renderToString(React.createElement(ReactForm, { owner: server }));
assert.match(markup, /SSR one/); assert.match(renderToString(React.createElement(ReactForm, { owner: other })), /SSR two/);
assert.equal(server.subscriptions, 0);
const hydration = document.createElement("div"); hydration.innerHTML = markup; document.body.append(hydration);
let hydrated;
await React.act(async () => { hydrated = hydrateRoot(hydration, React.createElement(ReactForm, { owner: server })); });
assert.equal(server.subscriptions, 1); await React.act(async () => { hydrated.unmount(); }); assert.equal(server.subscriptions, 0);
server.form.dispose(); other.form.dispose(); hydration.remove();

const vueOwner = owned("Vue initial");
const vueHost = document.createElement("div"); document.body.append(vueHost);
const app = Vue.createApp({ setup() { const state = useVueForm(vueOwner.store); return () => Vue.h("output", state.value.text[vueOwner.field.path] ?? state.value.values.body.name); } });
app.mount(vueHost); assert.equal(vueHost.textContent, "Vue initial"); assert.equal(vueOwner.subscriptions, 1);
vueOwner.field.setText("Vue draft"); await Vue.nextTick(); assert.equal(vueHost.textContent, "Vue draft");
vueOwner.field.parse(); await Vue.nextTick(); assert.equal(vueHost.textContent, "Vue draft");
app.unmount(); assert.equal(vueOwner.subscriptions, 0); vueOwner.field.set("still owned"); vueOwner.form.dispose(); vueHost.remove();
assert.throws(() => useVueForm(owned("outside").store), /active Vue scope/);
const scoped = owned("scope"), scope = Vue.effectScope();
const state = scope.run(() => useVueForm(scoped.store)); scoped.field.set("next"); assert.equal(state.value.values.body.name, "next");
scope.stop(); assert.equal(scoped.subscriptions, 0); scoped.form.dispose();
// A generic store may return a ref. The adapter must not reuse or unwrap it.
const firstRef = Vue.ref("first"), nextRef = Vue.ref("next"), refScope = Vue.effectScope();
let currentRef = firstRef, notifyRef, refStopped = false;
const outerRef = refScope.run(() => useVueForm({ getSnapshot: () => currentRef, subscribe(listener) { notifyRef = listener; return () => { refStopped = true; }; } }));
assert.notEqual(outerRef, firstRef); assert.equal(outerRef.value, firstRef);
currentRef = nextRef; notifyRef();
assert.equal(outerRef.value, nextRef); assert.equal(firstRef.value, "first"); assert.equal(nextRef.value, "next");
refScope.stop(); assert.equal(refStopped, true);
// Server rendering never mounts or stops component scopes, so it must not
// leave a subscription behind on a borrowed, longer-lived store.
const vueServer = owned("Vue SSR");
const serverApp = Vue.createSSRApp({ setup() { const snapshot = useVueForm(vueServer.store); return () => Vue.h("output", snapshot.value.values.body.name); } });
try {
  assert.match(await renderVue(serverApp), /Vue SSR/); assert.match(await renderVue(serverApp), /Vue SSR/);
  assert.equal(vueServer.subscriptions, 0);
} finally { vueServer.form.dispose(); }
// A scope stopped during setup, before its component mounts, never subscribes.
const early = owned("early"), earlyHost = document.createElement("div"); document.body.append(earlyHost);
const earlyApp = Vue.createApp({ setup() {
  const inner = Vue.effectScope(), s = inner.run(() => useVueForm(early.store)); inner.stop();
  return () => Vue.h("output", s.value.values.body.name);
} });
earlyApp.mount(earlyHost); assert.equal(earlyHost.textContent, "early"); assert.equal(early.subscriptions, 0);
earlyApp.unmount(); early.form.dispose(); earlyHost.remove();
dom.window.close();
console.log("PASS real React StrictMode/render/hydration and Vue mount/scope/SSR; subscriptions clean up without disposing borrowed controllers");
