package consumer_test

import (
	"testing"

	"foundry.test/consumer/catalog"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestCurrentEmailAssignmentSuppliesExactInput(t *testing.T) {
	for _, email := range []string{"typed@example.test", ""} {
		t.Run(email, func(t *testing.T) {
			user := models.User{Email: email}
			var assignment orm.Assignment[models.User] = user.CurrentEmailAssignment()
			user.Email = "changed after assignment"
			inspectInsertAssignments(t, models.Users.Table, []orm.Assignment[models.User]{assignment}, func(write *orm.Write[models.User]) {
				actual, present := orm.WriteGet(write, models.Users.Email.Set).Get()
				if !present || !orm.WriteIsSet(write, models.Users.Email.Set) || actual != email {
					t.Error("email assignment lost presence or its captured value")
				}
			})
		})
	}
}

func TestCreatorAssignmentSuppliesExactUserKey(t *testing.T) {
	for _, raw := range []string{"0193fd8c-2075-7000-8000-000000000001", "0193fd8c-2075-7000-8000-000000000002"} {
		t.Run(raw, func(t *testing.T) {
			id, err := model.ParseID[models.User](raw)
			if err != nil {
				t.Fatal(err)
			}
			product := catalog.Product{Code: "different-product-key", CreatorID: id}
			var assignment orm.Assignment[models.User] = product.CreatorAssignment()
			product.CreatorID = model.ID[models.User]{}
			inspectInsertAssignments(t, models.Users.Table, []orm.Assignment[models.User]{assignment}, func(write *orm.Write[models.User]) {
				actual, present := orm.WriteGet(write, models.Users.ID.Set).Get()
				if !present || !orm.WriteIsSet(write, models.Users.ID.Set) || actual != id {
					t.Error("creator assignment lost presence or its captured User key")
				}
			})
		})
	}
}
