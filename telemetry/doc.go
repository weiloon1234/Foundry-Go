// Package telemetry collects bounded, best-effort operational observations.
// A Collector owns its HTTP metrics independently of Prometheus and snapshots
// configuration at construction. Register its Report method with a recorder and
// the collector itself as an HTTP request observer. SetRoutes may be called once,
// before observation begins, with the assembled router's declared metadata.
//
// Collection performs no persistence. Browser reports are untrusted; applications
// still own authentication, authorization, CSRF, body limits and rate limiting.
// Loss counters are operational estimates, not an audit of unique lost requests:
// cumulative bucket snapshots and simultaneous capacity limits can overlap.
package telemetry
