package invalid

import "foundry.test/consumer/mutatorqueries"

func wrongNullable(member mutatorqueries.Member) mutatorqueries.DisplayNickname {
	nickname, _ := member.AccessNickname()
	return nickname
}
