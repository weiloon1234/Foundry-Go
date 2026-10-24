package invalid

import "foundry.test/consumer/passwords"

func bad() { _ = passwords.AccountRecords.Digest.Set("plaintext") }
