package positive

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

// Same spellings without the executor signature are ordinary application APIs.
type service struct{}

func (service) Query(string) string { return "" }
func (service) Exec()               {}
func Raw()                          {}
func Run(ctx context.Context, db database.Executor) error {
	Raw()
	service{}.Exec()
	_ = service{}.Query("ordinary application value")
	_, err := orm.All(ctx, db, models.Users.Query().Where(orm.TextEq(models.Users.Email, "Ada")))
	return err
}

type runner[R any] interface{ Query(string) R }
type harmless struct{}

func (harmless) Query(string) int { return 0 }
func unrelatedGeneric() {
	var r runner[int] = harmless{}
	_ = r.Query("safe")
	f := r.Query
	_ = f("safe")
}

func genericChannel[C ~chan int](channel C) { channel <- 1 }
