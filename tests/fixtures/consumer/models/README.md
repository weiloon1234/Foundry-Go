# Shared model declarations

`Users`, `Orders`, `Countries`, `Locations`, `Measurements`, `LedgerEntries`,
`Groups`, `Memberships`, `Friendships` and `WriteRecords` bind the original model
structs. Their keys retain their primary role for insert-select mappings; natural
keys and generated enums keep their original types. Existing column aliases and
database defaults come from the model tags.

Use `UserLinks`, `OrderLinks`, `CountryLinks`, `LocationLinks`, `GroupLinks` and
`MembershipLinks` for relations, and `UserValues` for aggregate slots. No model
has a custom mutator, managed timestamp or soft-delete role.

Generated APIs and their declaration directives remain during the Stage 9
publication barrier. They are used by unmigrated consumers and SQL parity oracles;
new consumers use these ORM declarations. The lead owns generated-output cleanup.

`user.CurrentEmailAssignment()` returns `orm.Assignment[User]` for the current
email, preserving an explicit empty string. For a product's creator key,
`product.CreatorAssignment()` returns an assignment for `models.User`.
The old draft helpers remain temporarily for lane D's root-test cutover.
