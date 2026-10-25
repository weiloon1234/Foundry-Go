# Lifecycle registrations

`CountObservers[M](set)` returns a `RegistrationCounts` value with `Write`,
`Retrieval` and `DeletionOnly` integer counts for exactly `M`. Inspection does not
construct callbacks or expose adapter types, factories or capabilities. Changing
the returned value cannot change registrations. Counts remain available through
`rows.Observers()` after row closure. Counting `set.ForDeletion()` describes that
merged view: `Write` includes deletion-only registrations and `DeletionOnly` is zero.
