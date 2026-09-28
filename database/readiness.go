package database

import "github.com/weiloon1234/Foundry-Go/health"

// ReadinessProbes contributes separate configured endpoint checks to the shared
// health registry. IDs belong to the application's declared health scope; the
// optional read ID is used only when that endpoint exists. These callbacks
// never execute migrations or inspect domain tables.
func (db *DB) ReadinessProbes(primary, read health.ProbeID) []health.Probe {
	probes := []health.Probe{{ID: primary, Check: db.PingPrimary}}
	if db.HasReadPool() {
		probes = append(probes, health.Probe{ID: read, Check: db.PingRead})
	}
	return probes
}
