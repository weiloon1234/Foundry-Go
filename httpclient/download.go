package httpclient

import (
	"context"
	"io"
	"net/http"
	"sync"

	"github.com/weiloon1234/Foundry-Go/internal/ownedstream"
)

// MaxDownloadBytes bounds one Download independently of Config.ResponseBytes.
const MaxDownloadBytes int64 = 1 << 40

// DownloadResult describes a completed download.
type DownloadResult struct {
	Status   int
	Headers  http.Header
	Bytes    int64
	Attempts int
}

// Download streams one successful (2xx) response body into destination under
// its own byte limit, for files larger than Config.ResponseBytes. Other
// statuses fail as StatusFailed without writing. The destination is borrowed
// and receives bytes as they arrive: a failed or interrupted download can leave
// a partial write, so write to a temporary file and publish it only on
// success. Retries follow the request policy until the body starts.
func (c *Client) Download(ctx context.Context, request Request, destination io.Writer, maximum int64) (DownloadResult, error) {
	if destination == nil || maximum < 1 || maximum > MaxDownloadBytes {
		return DownloadResult{}, invalid()
	}
	var result DownloadResult
	var statusErr error
	err := c.execute(ctx, request, maximum, func(op context.Context, info responseInfo, body *ownedstream.Body) error {
		if statusErr = info.ensureSuccess(); statusErr != nil {
			return statusErr
		}
		written, err := io.Copy(destination, body)
		if err != nil {
			return err
		}
		if err := op.Err(); err != nil {
			return err
		}
		result = DownloadResult{Status: info.status, Headers: info.headers.Clone(), Bytes: written, Attempts: info.attempt}
		return nil
	})
	if statusErr != nil {
		// An unsuccessful status is reported as StatusFailed, not a body failure.
		return DownloadResult{}, statusErr
	}
	if err != nil {
		return DownloadResult{}, err
	}
	return result, nil
}

// Outcome is one DoAll result in request order.
type Outcome struct {
	Response Response
	Err      error
}

// MaxBatchRequests bounds one DoAll call.
const MaxBatchRequests = 4096

// DoAll runs Do for each request with at most concurrency in flight and
// returns outcomes in request order. Each request still uses the client's own
// queued admission, deadlines and retry policy; one failure does not cancel
// the others. concurrency must not exceed Config.Concurrency.
func (c *Client) DoAll(ctx context.Context, requests []Request, concurrency int) ([]Outcome, error) {
	if c == nil || ctx == nil || len(requests) > MaxBatchRequests || concurrency < 1 || concurrency > c.config.Concurrency {
		return nil, invalid()
	}
	outcomes := make([]Outcome, len(requests))
	slots := make(chan struct{}, concurrency)
	var group sync.WaitGroup
	for i, request := range requests {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			for j := i; j < len(requests); j++ {
				outcomes[j].Err = ctx.Err()
			}
			group.Wait()
			return outcomes, nil
		}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			outcomes[i].Response, outcomes[i].Err = c.Do(ctx, request)
		}()
	}
	group.Wait()
	return outcomes, nil
}
