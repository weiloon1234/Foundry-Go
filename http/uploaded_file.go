package http

import "github.com/weiloon1234/Foundry-Go/internal/upload"

// UploadedFile is a fully captured, request-owned file. Open supplies a bounded
// lifetime native reader; Name, Size and content-type methods expose metadata.
// Temporary paths stay private. Files cannot be implicitly serialized as JSON
// or retained for a job after the request ends; persist them through storage.
type UploadedFile = upload.File
