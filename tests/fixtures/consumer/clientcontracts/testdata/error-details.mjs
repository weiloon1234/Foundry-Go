import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

const [full, members, older, guide] = await Promise.all(process.argv.slice(2, 6).map(path => import(pathToFileURL(path).href)));
const baseURL = process.argv[6];
const transport = async request => {
  const result = await fetch(request.url, {method:request.method,headers:request.headers,body:request.body,signal:request.signal});
  return {status:result.status,headers:Object.fromEntries(result.headers),body:result.body ?? new Uint8Array(),close:async()=>{if(result.body&&!result.body.locked) await result.body.cancel();}};
};
const fullClient=full.createClient(transport,{baseURL});
const expectedID="0193fd8c-2075-7000-8000-000000000002";
for (const sdk of [full,members,older]) {
  const api=sdk.createClient(transport,{baseURL});
  await assert.rejects(api.membersCreate({body:{name:"existing"}}),error=>{
    assert(error instanceof sdk.APIError);
    error.attemptNumber=1; error.cause=new Error("caller metadata"); Error.captureStackTrace(error);
    assert.equal(error.attemptNumber,1); assert.equal(error.cause.message,"caller metadata"); assert.equal(typeof error.stack,"string");
    assert(sdk.isOperationError("membersCreate",error));
    assert.equal(error.code,sdk.ErrorCode.MemberExists);
    assert.equal(error.status,409);
    assert.equal(error.response.details.existing_member_id,expectedID);
    assert.equal(error.response.details.existing.name,"Existing member");
    if(sdk===older) {
      assert(error.response.details.existing.state instanceof sdk.UnknownEnumValue);
      assert.equal(error.response.details.existing.state.value,"ready");
    } else assert.equal(error.response.details.existing.state,"ready");
    if(sdk===members) assert(full.isOperationError("membersCreate",error));
    assert(!sdk.isOperationError("membersIndex",error));
    return true;
  });
  assert.equal((await api.membersCreate({body:{name:"healthy"}})).id,expectedID);
  await assert.rejects(api.membersCreate({body:{name:"closed"}}),error=>{
    assert(sdk.isOperationError("membersCreate",error));
    assert.equal(error.code,sdk.ErrorCode.RegistrationClosed);
    assert(!Object.hasOwn(error.response,"details"));
    return true;
  });
  const form=sdk.createForm(sdk.operation("membersCreate"),{body:{name:"existing"}});
  const result=await form.submit(api);
  assert.equal(result.status,"failed"); assert.equal(result.outcome,"error_response");
  assert(sdk.isOperationError("membersCreate",result.error));
  assert.equal(result.error.response.details.existing_member_id,expectedID);
  assert.equal(form.getSnapshot().error,result.error);
  assert.equal(form.getSnapshot().issues[0].code,"member_exists");
  form.field(sdk.operation("membersCreate").field("body","name")).set("healthy");
  assert.equal((await form.submit(api)).status,"succeeded");
  form.dispose();
}
assert.equal(await guide.createMember(fullClient,"existing"),expectedID);
console.log("PASS real detailed errors, shared/portal narrowing, form failures, newer-server enum and compiled guide");
