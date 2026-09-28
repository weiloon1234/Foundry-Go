package invalid

import (
	"foundry.test/consumer/limiting"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
	"net/http"
)

func wrong(l limiting.MemberLimiter) {
	_ = limiting.MemberMiddleware(l, func(*http.Request) (model.ID[models.User], error) { return model.ID[models.User]{}, nil })
}
