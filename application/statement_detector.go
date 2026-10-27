package application

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

// StatementDetectorSettings enables request-scoped N+1 warnings. Disabled installs
// nothing. Outside the "development" namespace, AllowNonDevelopment is required.
// Example: settings.HTTP.StatementDetector.Enabled = true.
type StatementDetectorSettings struct {
	Enabled             bool
	AllowNonDevelopment bool
	Limits              database.StatementDetectorConfig
}

func prepareStatementDetector(s Settings, logger *slog.Logger) ([]infrastructure.Option, []http.Middleware, error) {
	c := s.HTTP.StatementDetector
	if !c.Enabled {
		return nil, nil, nil
	}
	if !s.HTTP.Enabled {
		return nil, nil, safeerror.NewFault(fault.Invalid, "request statement detector requires HTTP")
	}
	if s.Services.Namespace.Environment != "development" && !c.AllowNonDevelopment {
		return nil, nil, safeerror.NewFault(fault.Invalid, "statement detector outside development requires explicit AllowNonDevelopment")
	}
	d, err := database.NewStatementDetector(c.Limits)
	if err != nil {
		return nil, nil, err
	}
	return []infrastructure.Option{infrastructure.WithStatementDetector(d)}, []http.Middleware{http.StatementDetector(d, logger)}, nil
}
