package developeroperations

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/logging"
)

func Inspect(ctx context.Context, services application.Services, channel logging.ChannelName) ([]logging.FileInfo, error) {
	owner, err := services.LogFiles.FileSink(channel)
	if err != nil {
		return nil, err
	}
	return owner.Files(ctx)
}
func Observer() (http.RequestObserver, error) { return http.NewRequestMetrics(512) }
