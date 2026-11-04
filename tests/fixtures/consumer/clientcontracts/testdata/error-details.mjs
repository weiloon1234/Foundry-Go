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
for (const sdk of [full,members,older]) for (const strictResponses of sdk===older?[false]:[false,true]) {
  const api=sdk.createClient(transport,{baseURL,strictResponses});
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
const plainResponse=await fetch(baseURL+"/members",{method:"POST",headers:{"content-type":"application/json"},body:JSON.stringify({name:"closed"})});
assert.equal(plainResponse.status,409);
const plainWire=await plainResponse.json(); assert.equal(plainWire.error_code,"registration_closed"); assert(!Object.hasOwn(plainWire,"details"));
let rejected=0, delivered=0;
const privateBoundaries=new Map(["budgetUnion","budgetCollections","budgetOptional"].map(name=>[name,{rejected:0,delivered:0,first:undefined}]));
for(let steps=15;steps<=48;steps++) {
  const sdk=await import(new URL("./steps-"+steps+".js",pathToFileURL(process.argv[2])).href);
  let last;
  const api=sdk.createClient(async request=>{
    const response=await fetch(request.url,{method:request.method,headers:{...request.headers,"X-Fixture-Steps":String(steps)},body:request.body});
    const body=await response.text(); last={status:response.status,body};
    return {status:response.status,headers:Object.fromEntries(response.headers),body,close(){}};
  },{baseURL:baseURL+"/details-budget",strictResponses:true});
  await assert.rejects(api.membersCreate({body:{name:"existing"}}),error=>{
    assert(error instanceof sdk.APIError,`Steps ${steps}: ${error.name}`);
    assert(sdk.isOperationError("membersCreate",error));
    assert.equal(error.response.request_id,"11111111-1111-4111-8111-111111111111");
    const raw=JSON.parse(last.body);
    if(last.status===409) {
      delivered++; assert.equal(error.code,"member_exists"); assert.equal(error.response.details.existing_member_id,expectedID);
    } else {
      rejected++; assert.equal(last.status,500); assert.equal(raw.error_code,"internal_error"); assert(!Object.hasOwn(raw,"details"));
      assert(!last.body.includes(expectedID)); assert(!last.body.includes("private-member-cause"));
    }
    return true;
  });
  assert.equal((await api.membersCreate({body:{name:"healthy"}})).id,expectedID);
  for(const [name,boundary] of privateBoundaries) {
    await assert.rejects(api[name]({}),error=>{
      const raw=JSON.parse(last.body);
      assert(error instanceof sdk.APIError,`${name} Steps ${steps}: ${error.name}`);
      assert(sdk.isOperationError(name,error));
      assert(!Object.hasOwn(raw,"request_id")); assert(!Object.hasOwn(raw,"issues"));
      if(last.status===409) {
        boundary.delivered++; boundary.first??=steps;
        assert.deepEqual({...error.response.details.choice},{kind:"existing",label:"x"});
        if(name!=="budgetUnion") {
          assert.deepEqual(error.response.details.array.map(item=>({...item})),[{kind:"existing",label:"a"}]);
          assert.deepEqual({...error.response.details.map.first},{kind:"existing",label:"m"});
          assert.equal(Object.hasOwn(error.response.details,"note"),name==="budgetOptional");
          if(name==="budgetOptional") assert.equal(error.response.details.note,"note");
        }
      } else {
        boundary.rejected++; assert.equal(last.status,500); assert.equal(raw.error_code,"internal_error");
        assert(!Object.hasOwn(raw,"details")); assert(!last.body.includes('"existing"'));
      }
      return true;
    });
  }
}
assert(rejected>0); assert(delivered>0);
for(const [name,boundary] of privateBoundaries) {
  assert(boundary.rejected>0,name); assert(boundary.delivered>0,name);
  console.log(`${name}: first server 409 at Steps ${boundary.first}; all 409s decoded`);
}
assert.equal(privateBoundaries.get("budgetUnion").first,24);
assert.equal(await guide.createMember(fullClient,"existing"),expectedID);
console.log("PASS real detailed errors, shared/portal narrowing, form failures, newer-server enum and compiled guide");
