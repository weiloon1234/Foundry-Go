package invalid

import "foundry.test/consumer/passwords"

func bad() { _ = passwords.AccountFields().Digest.Like("%hash%") }
