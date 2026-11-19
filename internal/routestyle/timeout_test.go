package routestyle

import (
	"context"
	"go/ast"
	"go/types"
	"testing"
)

type inspectionDeadline struct {
	context.Context
	remaining int
}

func (c *inspectionDeadline) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.DeadlineExceeded
	}
	return nil
}
func TestDeadlineDuringIndexAndInspection(t *testing.T) {
	source := `package routes;import h "github.com/weiloon1234/Foundry-Go/http";func use(){a:=h.DefineEndpoint[h.Route];b:=h.DefineEndpoint[h.Route];c:=h.DefineEndpoint[h.Route];_=a(h.DefineRoute());_=b(h.DefineRoute());_=c(h.DefineRoute())}`
	a, f := testAnalysis(t, source)
	a.aliases = make(map[types.Object][]ast.Expr)
	a.Context = &inspectionDeadline{Context: context.Background(), remaining: 3}
	a.Index(f)
	if len(a.aliases) != 0 {
		t.Fatal("indexing ignored deadline")
	}
	a.Context = &inspectionDeadline{Context: context.Background(), remaining: 3}
	if got := a.CheckFile(f); len(got) != 0 {
		t.Fatal("inspection ignored deadline", got)
	}
}
