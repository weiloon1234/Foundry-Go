package generate

import "go/types"

// customDTOContract discovers an explicit native-codec contract from the value
// method set. It never executes application code during generation. A pointer
// receiver is rejected because construction has no hydrated model/value.
func customDTOContract(typ *types.Named) (bool, string) {
	found := false
	methods := types.NewMethodSet(types.NewPointer(typ))
	for i := 0; i < methods.Len(); i++ {
		method := methods.At(i).Obj()
		if method.Name() != "JSONContract" {
			continue
		}
		found = true
		sig, ok := method.Type().(*types.Signature)
		if !ok || sig.Variadic() || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
			return false, "JSONContract must be a value method returning contract.JSON of the same type"
		}
		if _, pointer := sig.Recv().Type().(*types.Pointer); pointer {
			return false, "JSONContract must be a value method independent of receiver state"
		}
		result, ok := types.Unalias(sig.Results().At(0).Type()).(*types.Named)
		if !ok || !isNamed(result, framework+"/contract", "JSON") ||
			result.TypeArgs().Len() != 1 || !types.Identical(result.TypeArgs().At(0), typ) {
			return false, "JSONContract must return contract.JSON of the same concrete value type"
		}
	}
	if !found {
		return false, ""
	}
	if !typ.Obj().Exported() {
		return false, "JSONContract values must be exported named types"
	}
	// Require an encoder on the value (not just *T), and a decoder used by the
	// existing native decoder, including its streaming JSON value protocol.
	encode := false
	methods = types.NewMethodSet(typ)
	for i := 0; i < methods.Len(); i++ {
		method := methods.At(i).Obj()
		switch method.Name() {
		case "MarshalJSON", "MarshalText", "MarshalJSONTo", "AppendText":
			encode = encode || jsonCodecSignature(method.Name(), method.Type().(*types.Signature))
		}
	}
	decode := false
	methods = types.NewMethodSet(types.NewPointer(typ))
	for i := 0; i < methods.Len(); i++ {
		method := methods.At(i).Obj()
		switch method.Name() {
		case "UnmarshalJSON", "UnmarshalJSONFrom", "UnmarshalText":
			decode = decode || jsonCodecSignature(method.Name(), method.Type().(*types.Signature))
		}
	}
	if !encode || !decode {
		return false, "JSONContract requires a value JSON/text encoder and a native JSON/text decoder on its pointer"
	}
	return true, ""
}
