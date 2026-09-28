package datatable

import (
	"context"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
)

func TestManagerRejectsInvalidResourceConfigurationAtConstruction(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"schema":          func(c *Config) { c.Schema = "public;SELECT" },
		"query capacity":  func(c *Config) { c.MaxActive = 0 },
		"query timeout":   func(c *Config) { c.Timeout = 0 },
		"page size":       func(c *Config) { c.MaxPageSize = DefaultPageSize - 1 },
		"offset":          func(c *Config) { c.MaxOffset = -1 },
		"row bytes":       func(c *Config) { c.MaxRowBytes = 0 },
		"page bytes":      func(c *Config) { c.MaxPageBytes = c.MaxRowBytes - 1 },
		"export capacity": func(c *Config) { c.MaxExports = 0 },
		"export timeout":  func(c *Config) { c.ExportTimeout = time.Hour + 1 },
		"export rows":     func(c *Config) { c.MaxExportRows = maxWorksheetRows },
		"export bytes":    func(c *Config) { c.MaxExportBytes = 0 },
		"XML bytes":       func(c *Config) { c.MaxXMLBytes = 0 },
		"cell bytes":      func(c *Config) { c.MaxCellBytes = 0 },
		"relative path":   func(c *Config) { c.TempDir = "relative" },
	} {
		t.Run(name, func(t *testing.T) {
			config := DefaultConfig()
			change(&config)
			if manager, err := New(Dependencies{Database: new(database.DB)}, config); err == nil || manager != nil {
				t.Fatal("invalid resource configuration constructed manager")
			}
		})
	}
	if manager, err := New(Dependencies{}, DefaultConfig()); err == nil || manager != nil {
		t.Fatal("missing database accepted")
	}
	if manager, err := New(Dependencies{Database: new(database.DB)}, DefaultConfig(), Define(reportSpec()).Registration()); err == nil || manager != nil {
		t.Fatal("export without locale/labels accepted")
	}
	var manager *Manager
	if manager.Validate() == nil || manager.Close(context.Background()) == nil {
		t.Fatal("nil manager accepted")
	}
	select {
	case <-manager.DoneQueries():
	default:
		t.Fatal("nil manager retained query lifetime")
	}
	select {
	case <-manager.DoneExports():
	default:
		t.Fatal("nil manager retained export lifetime")
	}
}
