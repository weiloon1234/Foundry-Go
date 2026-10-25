package invalid

import a "foundry.test/goway/models/identity"

var wrong string = a.MemberIdentity.ReferenceOf(a.Member{}).Key()
