import { createForm, operation } from "./contracts_foundry.gen.js";
import { useForm as useReactForm } from "./contracts_react_foundry.gen.js";
import { useForm as useVueForm } from "./contracts_vue_foundry.gen.js";
import { ref } from "vue";
import type { Ref } from "vue";

function consumer() {
  const form = createForm(operation("formsSubmit"), { body: { name: "", "tags[]": [] } });
  const react = useReactForm(form);
  const name: string | undefined = react.values.body?.name;
  const vue = useVueForm(form);
  const vueName: string | undefined = vue.value.values.body?.name;
  const task = form.task(async values => [values.body?.name ?? ""]);
  const choices: readonly string[] | undefined = useReactForm(task).value;
  const vueChoices: readonly string[] | undefined = useVueForm(task).value.value;
  const inner = ref("borrowed");
  const outer = useVueForm({ getSnapshot: () => inner, subscribe: () => () => {} });
  const preservedRef: Ref<string> = outer.value;
  // @ts-expect-error The hook does not erase the operation's request type.
  react.values.body?.missingField;
  // @ts-expect-error Vue's ref is read-only.
  vue.value = vue.value;
  // @ts-expect-error Snapshots cannot be mutated through the adapter.
  react.values.body!.name = "bad";
  void [name, vueName, choices, vueChoices, preservedRef];
}
void consumer;
