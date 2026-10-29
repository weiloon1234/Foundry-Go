package models

import "github.com/weiloon1234/Foundry-Go/database/orm"

// CurrentEmailAssignment supplies the user's email, including an explicit empty string.
// Example: orm.Insert(ctx, db, Users.Table, user.CurrentEmailAssignment()).
func (u User) CurrentEmailAssignment() orm.Assignment[User] {
	return orm.TextSet(Users.Email, u.Email)
}
