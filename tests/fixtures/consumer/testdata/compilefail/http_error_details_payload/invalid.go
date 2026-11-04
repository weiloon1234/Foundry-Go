package invalid

import "foundry.test/consumer/clientcontracts"

var _ = clientcontracts.MemberExists.New(clientcontracts.MemberCreated{})
