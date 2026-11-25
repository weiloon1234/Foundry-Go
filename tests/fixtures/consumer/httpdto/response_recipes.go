package httpdto

import (
	"context"

	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func encodeUser(ctx context.Context, userID model.ID[models.User], email string, activeState models.Status, limits contract.JSONLimits) ([]byte, error) {
	// doc-example:begin http-responses-1
	response := UserResponse{ID: userID, Email: email, State: activeState}
	return UserResponseJSON().Encode(ctx, response, limits)
	// doc-example:end http-responses-1
}

func encodeUsers(ctx context.Context, response UserResponse, limits contract.JSONLimits) ([]byte, error) {
	// doc-example:begin http-responses-2
	users := contract.Slice(UserResponseJSON())
	return users.Encode(ctx, []UserResponse{response}, limits)
	// doc-example:end http-responses-2
}

func encodePresentUser(ctx context.Context, response UserResponse, limits contract.JSONLimits) ([]byte, error) {
	// doc-example:begin http-responses-2-2
	optionalUser := contract.Nullable(UserResponseJSON())
	return optionalUser.Encode(ctx, value.Of(response), limits)
	// doc-example:end http-responses-2-2
}
func encodeNullUser(ctx context.Context, limits contract.JSONLimits) ([]byte, error) {
	// doc-example:begin http-responses-2-3
	optionalUser := contract.Nullable(UserResponseJSON())
	return optionalUser.Encode(ctx, value.Null[UserResponse](), limits)
	// doc-example:end http-responses-2-3
}
