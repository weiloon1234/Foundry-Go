package pagination_test

import (
	"context"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type privateCursorCodecError struct{ formatted int }

func (e *privateCursorCodecError) Error() string { e.formatted++; return "private cursor codec" }
func (e *privateCursorCodecError) Format(s fmt.State, _ rune) {
	e.formatted++
	fmt.Fprint(s, "private cursor formatter")
}

func TestCursorEncoderAndDecoderInfrastructureFailuresStayHTTP500(t *testing.T) {
	encoded, err := querycore.EncodeCursorValue(int64(7))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(querycore.CursorEnvelope{Version: 1, Scope: strings.Repeat("a", 64), Values: []querycore.CursorValue{encoded}})
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(wire)
	for _, phase := range []string{"encode", "decode"} {
		for _, invalid := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/invalid=%v", phase, invalid), func(t *testing.T) {
				private := &privateCursorCodecError{}
				var cause error = private
				if invalid {
					cause = fault.Wrap(fault.Invalid, "invalid key", private)
				}
				c := codec.New(func(v int64) (driver.Value, error) {
					if phase == "encode" {
						return nil, cause
					}
					return v, nil
				}, func(any) (int64, error) {
					if phase == "decode" {
						return 0, cause
					}
					return 7, nil
				})
				response := serve(t, pagination.Handle(cursorEndpoint(pagination.DefaultCursorConfig()), func(context.Context, cursorInput) (pagination.CursorResult[cursorSource, Item], error) {
					_, err := querycore.ReadCursorBoundary(token, true, false, strings.Repeat("a", 64), []querycore.CursorField{{Decode: func(raw any) (driver.Value, error) {
						v, err := c.Decode(raw)
						if err != nil {
							return nil, err
						}
						return c.Bind(v)
					}}}, []bool{true})
					if !errors.Is(err, private) {
						t.Error("cursor cause lost")
					}
					return pagination.CursorResult[cursorSource, Item]{}, err
				}), "/items")
				want := 500
				if invalid {
					want = 400
				}
				if response.Code != want || private.formatted != 0 {
					t.Fatal("cursor codec classification or redaction failed", response.Code)
				}
			})
		}
	}
}
