// Package sqlowner seals framework database-owner capabilities to this module.
// Methods accepting Seal can be declared only by Foundry packages, so typed
// query execution can trust an owner capability without a closed type switch.
package sqlowner

// Seal can only be named inside this module. A wrapper that embeds a framework
// owner promotes its sealed methods and therefore claims that owner's metadata;
// wrappers that replace transaction ownership must not embed the owner.
type Seal struct{ _ struct{} }
