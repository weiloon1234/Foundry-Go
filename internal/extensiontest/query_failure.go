package extensiontest

import (
	"context"
	"database/sql/driver"
)

// InterceptQueries injects a selected query failure while forwarding the real
// driver's execution and connection-lifetime interfaces used by the fixture.
func InterceptQueries(base driver.Connector, failure func(string) error) driver.Connector {
	return queryFailureConnector{Connector: base, failure: failure}
}

type queryFailureConnector struct {
	driver.Connector
	failure func(string) error
}

func (c queryFailureConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &queryFailureConnection{countConnection: &countConnection{Conn: conn}, failure: c.failure}, nil
}

type queryFailureConnection struct {
	*countConnection
	failure func(string) error
}

func (c *queryFailureConnection) QueryContext(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.failure(statement); err != nil {
		return nil, err
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, statement, args)
}
