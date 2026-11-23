import * as sdk from "./contracts_foundry.gen.js";

// doc-example:begin form-identity-text
export function formIdentityText(issues: readonly sdk.Issue[]) {
  type FormBody = sdk.Operations["itemsEcho"]["request"]["body"];
  type Draft = { readonly owner_id?: FormBody["owner_id"] | undefined };
  let values: Draft = {};
  const update: sdk.FormUpdate<Draft> = (next) => { values = next(values); };
  const form = sdk.formFieldState();
  const fields = sdk.fieldBinder((key) => key, () => form);
  const owner = fields(sdk.operation("itemsEcho"), values, update, issues).owner_id;
  sdk.commitFormFields(form);
  // The control keeps the raw text the user is typing, writes the branded ID
  // when valid (or undefined otherwise), and treats non-empty invalid text as
  // an input error that blocks submit rather than silently omitting it.
  let text = owner.value ?? "";
  const invalid = (): boolean => owner.identity && text !== "" && sdk.fieldIdentity(owner, text) === undefined;
  const onTextChange = (next: string) => {
    text = next;
    if (owner.identity) owner.onChange(sdk.fieldIdentity(owner, next));
  };
  const submit = (): { readonly owner_id: FormBody["owner_id"] } | undefined => {
    if (invalid() || values.owner_id === undefined) return undefined;
    return { owner_id: values.owner_id };
  };
  return { owner, getText: () => text, invalid, onTextChange, submit };
}
// doc-example:end form-identity-text
