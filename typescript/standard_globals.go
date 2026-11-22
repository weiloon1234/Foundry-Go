package typescript

// A generated type takes its plain Go name unless that name is reserved, in
// which case it is qualified by its package (datatable.Request becomes
// Datatable_Request). The reserved set is exactly:
//
//   - every name the generated module declares at module scope: the
//     renderer's declarations and the runtime's;
//   - every global name the module's code refers to, which a declaration of
//     the same name would shadow inside the module;
//   - standardGlobalNames, below: global types that application files
//     commonly use beside imported generated types, so an import of a
//     generated type must never shadow one of them.
//
// Comments, string text, property names and type parameters reserve nothing.
// Changing the set renames consumer types, so it changes only with a
// compatibility note.

// standardGlobalNames are the fetch, DOM, TypeScript and ECMAScript global
// type names a generated type never takes.
var standardGlobalNames = []string{
	// fetch and DOM
	"Animation", "Blob", "Cache", "Comment", "Document", "Element", "Event", "File", "FormData",
	"Headers", "History", "Image", "Location", "Node", "Notification", "Option", "Range",
	"Request", "Response", "Screen", "Selection", "Storage", "Text", "Touch", "URL", "Worker",
	// TypeScript and ECMAScript
	"Array", "Awaited", "Boolean", "Date", "Error", "Exclude", "Extract", "Function", "Iterator",
	"Map", "NonNullable", "Number", "Object", "Omit", "Parameters", "Partial", "Pick", "Promise",
	"Readonly", "Record", "Required", "ReturnType", "Set", "String", "Symbol",
}
