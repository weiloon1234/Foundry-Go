package outgoing

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/httpclient"
)

func exportAccounts(ctx context.Context, client *httpclient.Client, destination io.Writer) error {
	// doc-example:begin http-client-3
	err := client.Stream(ctx, client.Get("accounts/export"),
		func(ctx context.Context, response *httpclient.StreamResponse) error {
			if err := response.EnsureSuccess(); err != nil {
				return err
			}
			_, err := io.Copy(destination, response)
			return err
		},
	)
	// doc-example:end http-client-3
	return err
}

func publicClient() (*httpclient.Client, error) {
	// doc-example:begin http-client-4
	settings := httpclient.DefaultConfig("preview")
	settings.Destination = httpclient.PublicDestinations()      // HTTPS, port 443
	settings.Destination.Hosts = []string{"images.example.com"} // optional exact names
	client, err := httpclient.New(settings, nil)
	// doc-example:end http-client-4
	return client, err
}
