package invalid

import "foundry.test/consumer/clientcontracts"

var _ error = clientcontracts.MemberExists
