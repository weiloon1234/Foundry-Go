package invalid

import (
	"context"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/httpsigned"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"time"
)

var wrong model.ID[models.Order]
var links httpsigned.Links
var _, _ = links.Preview.URL(context.Background(), foundryhttp.Origin("https://example.test"), httpkernel.UserPath{User: wrong}, httpquery.SearchInput{}, time.Time{})
