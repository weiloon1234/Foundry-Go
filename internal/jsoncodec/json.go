// Package jsoncodec contains application JSON methods before native JSON error
// inspection. Foreign codecs receive opaque failures; callers receive safe,
// inspectable causes after the codec has returned.
package jsoncodec

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func Marshal(input any, options ...jsonv2.Options) (data []byte, err error) {
	err = callback.Isolated("marshal JSON", func() error {
		state := marshalState{rootAddressable: reflect.ValueOf(input).IsValid() && reflect.TypeOf(input).Kind() == reflect.Pointer}
		data, err = jsonv2.Marshal(input, marshalOptions(&state, options)...)
		return state.finish("JSON encoding failed", err)
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

func MarshalWrite(output io.Writer, input any, options ...jsonv2.Options) error {
	return callback.Isolated("marshal JSON", func() error {
		state := marshalState{rootAddressable: reflect.ValueOf(input).IsValid() && reflect.TypeOf(input).Kind() == reflect.Pointer}
		return state.finish("JSON encoding failed", jsonv2.MarshalWrite(output, input, marshalOptions(&state, options)...))
	})
}

func Unmarshal(data []byte, output any, options ...jsonv2.Options) error {
	return callback.Isolated("unmarshal JSON", func() error {
		state := unmarshalState{}
		return state.finish("JSON decoding failed", jsonv2.Unmarshal(data, output, unmarshalOptions(&state, options, false)...))
	})
}

func marshalOptions(state *marshalState, options []jsonv2.Options) []jsonv2.Options {
	result := make([]jsonv2.Options, 0, len(options)+2)
	result = append(result, json.DefaultOptionsV1())
	result = append(result, options...)
	return append(result, jsonv2.WithMarshalers(jsonv2.MarshalToFunc[any](state.marshal)))
}

// UnmarshalExact retains json.Number at dynamic interface boundaries.
func UnmarshalExact(data []byte, output any, options ...jsonv2.Options) error {
	return callback.Isolated("unmarshal JSON", func() error {
		state := unmarshalState{}
		return state.finish("JSON decoding failed", jsonv2.Unmarshal(data, output, unmarshalOptions(&state, options, true)...))
	})
}

func unmarshalNumber(dec *jsontext.Decoder, output *any) error {
	if *output != nil {
		return errors.ErrUnsupported
	}
	data, err := dec.ReadValue()
	if err != nil {
		return err
	}
	native := json.NewDecoder(bytes.NewReader(data))
	native.UseNumber()
	return native.Decode(output)
}

func unmarshalOptions(state *unmarshalState, options []jsonv2.Options, exact bool) []jsonv2.Options {
	result := make([]jsonv2.Options, 0, len(options)+2)
	result = append(result, json.DefaultOptionsV1())
	result = append(result, options...)
	methods := jsonv2.UnmarshalFromFunc[any](state.unmarshal)
	if exact {
		methods = jsonv2.JoinUnmarshalers(methods, jsonv2.UnmarshalFromFunc[*any](unmarshalNumber))
	}
	return append(result, jsonv2.WithUnmarshalers(methods))
}
