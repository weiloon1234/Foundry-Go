package ormcore

import "reflect"

func integerKind(k reflect.Kind) bool {
	return k >= reflect.Int && k <= reflect.Int64 || k >= reflect.Uint && k <= reflect.Uint64
}
func numericKind(k reflect.Kind) bool {
	return integerKind(k) || k == reflect.Float32 || k == reflect.Float64
}
