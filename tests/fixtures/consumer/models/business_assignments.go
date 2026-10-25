package models

import "github.com/weiloon1234/Foundry-Go/database/orm"

// CurrentEmailAssignment supplies the user's email, including an explicit empty string.
// Example: Users.Insert(ctx, db, user.CurrentEmailAssignment()).
func (u User) CurrentEmailAssignment() orm.Assignment[User] {
	return Users.Email.Set(u.Email)
}
