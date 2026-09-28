package invalid

import "foundry.test/consumer/mutatorqueries"

func wrongResult(member mutatorqueries.Member) int {
	email, _ := member.AccessEmail()
	return email
}
