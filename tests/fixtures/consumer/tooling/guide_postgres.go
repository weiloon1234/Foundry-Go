package tooling

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/postgres"
)

func enumConnection() postgres.Config {
	// doc-example:begin postgresql-1
	type OrderStatus string

	config := postgres.DefaultConfig()
	// Supply endpoint and credentials from the application's typed configuration.
	config.RegisterTypes = func(ctx context.Context, types *postgres.TypeRegistry) error {
		status, err := types.LoadType(ctx, "app.order_status")
		if err != nil {
			return err
		}
		if err := types.RegisterType(status); err != nil {
			return err
		}
		return types.RegisterDefaultPgType(OrderStatus(""), status.Name)
	}
	// doc-example:end postgresql-1
	return config
}
