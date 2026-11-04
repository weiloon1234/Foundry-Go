import * as sdk from "./contracts_foundry.gen.js";
import * as members from "./contracts_members_foundry.gen.js";

// Compiled guide example: the operation and code select one concrete payload.
export async function createMember(api: sdk.API, name: string) {
  try {
    return (await api.membersCreate({ body: { name } })).id;
  } catch (error) {
    if (sdk.isOperationError("membersCreate", error) && error.code === sdk.ErrorCode.MemberExists) {
      return error.response.details.existing_member_id;
    }
    throw error;
  }
}

export function errorDetailsTypes(error: unknown): void {
  // @ts-expect-error A caught value needs narrowing.
  error.response.details;
  if (sdk.isOperationError("membersCreate", error)) {
    const correlated: sdk.Operations["membersCreate"]["error"] = error;
    // @ts-expect-error The operation also declares plain errors.
    error.response.details;
    if (error.code === sdk.ErrorCode.MemberExists) {
      const id: sdk.Operations["membersCreate"]["response"]["id"] = error.response.details.existing_member_id;
      const name: string = error.response.details.existing.name;
      void [id, name];
    }
    if (error.code === sdk.ErrorCode.RegistrationClosed) {
      // @ts-expect-error A plain error has no details.
      error.response.details;
    }
    void correlated;
  }
  if (members.isOperationError("membersCreate", error)) {
    // @ts-expect-error A portal operation still needs code narrowing.
    error.response.details;
    if (error.code === members.ErrorCode.MemberExists) {
      const id: members.Operations["membersCreate"]["response"]["id"] = error.response.details.existing_member_id;
      const fullID: sdk.Operations["membersCreate"]["response"]["id"] = id;
      void fullID;
    }
    if (error.code === members.ErrorCode.RegistrationClosed) {
      // @ts-expect-error Plain variants have no details in portals either.
      error.response.details;
    }
  }
  // @ts-expect-error The members portal cannot name another portal's operation.
  members.isOperationError("accountShow", error);
}
