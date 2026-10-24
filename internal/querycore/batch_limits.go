package querycore

// MaxPageSize bounds complete collections returned by a pagination request.
const MaxPageSize = 1000

// MaxChunkSize shares the collection bound across both model read APIs.
const MaxChunkSize = MaxPageSize

// DefaultChunkSize bounds closed batches used by eager and observed iteration.
const DefaultChunkSize = 100

// MaxInsertRows bounds atomic inserts and upserts before parameter validation.
const MaxInsertRows = 1000

// MaxPerModelWriteRows bounds an atomic batch of ordinary model lifecycles.
const MaxPerModelWriteRows = MaxInsertRows
