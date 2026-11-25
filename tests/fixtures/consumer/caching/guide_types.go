package caching

import "foundry.test/consumer/mutatorqueries"

// Member retains the application's concrete model and its typed email accessor.
type Member = mutatorqueries.Member

// DisplayEmail is the application's email value returned by Member.AccessEmail.
type DisplayEmail = mutatorqueries.DisplayEmail
