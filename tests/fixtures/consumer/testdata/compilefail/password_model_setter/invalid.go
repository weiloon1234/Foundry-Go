package invalid

import "foundry.test/consumer/passwords"

func bad() { _ = passwords.AccountDraft{}.SetDigest("plaintext") }
