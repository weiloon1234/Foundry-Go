package query

import (
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestStatementRetainsSQLAndBinaryArgumentsPrivately(t *testing.T) {
	const secret = "statement-raw-SECRET-945817"
	sql := "SELECT '" + secret + "', $1"
	statement := statementFromCore(querycore.Statement{SQLText: sql, Bindings: []any{[]byte(secret)}})
	decimal := strings.Trim(fmt.Sprintf("%v", []byte(secret)), "[]")
	hex := fmt.Sprintf("%#v", []byte(secret))
	hex = strings.TrimSuffix(hex[strings.IndexByte(hex, '{')+1:], "}")
	for _, input := range []any{statement, &statement, struct{ private Statement }{statement}, struct{ nested struct{ private Statement } }{struct{ private Statement }{statement}}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			for _, diagnostic := range []string{fmt.Sprintf(format, input), fmt.Errorf("statement: "+format, input).Error()} {
				for _, sentinel := range []string{secret, decimal, hex} {
					if strings.Contains(diagnostic, sentinel) {
						t.Fatalf("%s disclosed SQL or argument bytes", format)
					}
				}
			}
		}
	}
	arguments := statement.Arguments()
	arguments[0].([]byte)[0] = '!'
	arguments[0] = nil
	if statement.SQL() != sql || string(statement.Arguments()[0].([]byte)) != secret {
		t.Fatal("explicit statement accessor ownership changed")
	}
}
