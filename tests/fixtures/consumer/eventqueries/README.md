# Model events

Both domain modules register ORM observers for `observerqueries.Plain`. Each
application installs only the selected observer family. `Created` receives stored
changes and the business transaction: in-memory dispatch uses `Created.AfterCommit`,
and durable delivery uses `Created.Enqueue` in that transaction.

`Links` persists payload-owned outbox IDs through an ORM ID column. The PostgreSQL
assertions retain outer rollback, enqueue failure, outbox configuration, listener
failure and committed-outcome checks. Domain event DTOs and topics are unchanged.
