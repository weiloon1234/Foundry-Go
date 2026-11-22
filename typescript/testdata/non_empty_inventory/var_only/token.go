package varonly

import "github.com/weiloon1234/Foundry-Go/contract"

// Token tests package-level var DefineScalar discovery independently of
// Parse/Format or Unmarshal* methods.
type Token string

var TokenScalar = contract.DefineScalar[Token](contract.Type{
	ID:   "varonly.Token",
	Kind: contract.StringKind,
})
