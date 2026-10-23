package ormcore

import "testing"

type binaryInput []byte

func TestInputCloneOwnsNamedBuffers(t *testing.T) {
	clone := InputClone[binaryInput]()
	input := binaryInput{1, 2}
	copy := clone(input)
	input[0] = 9
	if copy[0] != 1 {
		t.Fatal("input aliases caller buffer")
	}
	if clone(nil) != nil {
		t.Fatal("nil became non-nil")
	}
	if InputClone[string]() != nil {
		t.Fatal("immutable value needs no clone")
	}
}
