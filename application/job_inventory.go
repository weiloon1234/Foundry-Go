package application

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net"
	"slices"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/inspection"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/clidiag"
	"github.com/weiloon1234/Foundry-Go/internal/keyaddress"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/jobs"
	jobcommand "github.com/weiloon1234/Foundry-Go/jobs/command"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const (
	// JobQueueInventorySchema is the wire schema version emitted by deploy:jobs-inspect.
	JobQueueInventorySchema = 1
	// MaxJobQueueConnections bounds the number of Redis job connections in an inventory report.
	MaxJobQueueConnections = 64
	// MaxJobQueues bounds the total number of logical queues across all Redis job connections.
	MaxJobQueues = 4096
	// MaxJobQueueKeys bounds the number of physical Redis keys emitted for a single logical queue.
	MaxJobQueueKeys = 64
	// MaxJobQueueBlockers bounds the number of blockers in an inventory report.
	MaxJobQueueBlockers = 4096
	// MaxJobQueueInventoryBytes bounds the total stdout size of a job queue inventory report.
	MaxJobQueueInventoryBytes = 4 << 20
)

// JobQueueCoverage identifies the completeness boundary of the emitted queue inventory.
type JobQueueCoverage string

const (
	// JobQueueCoverageDeclared reports that queue names were derived from the Builder declaration graph.
	JobQueueCoverageDeclared JobQueueCoverage = "declared"
)

// JobQueueBlockerCode is a machine-readable reason why job queue cutover cannot proceed safely.
type JobQueueBlockerCode string

const (
	// JobQueueBlockerOpaqueProvider reports that custom providers or plugins may register opaque job queues.
	JobQueueBlockerOpaqueProvider JobQueueBlockerCode = "opaque_job_provider"
	// JobQueueBlockerInvalidConfiguration reports invalid settings or job/worker/event/outbox declarations.
	JobQueueBlockerInvalidConfiguration JobQueueBlockerCode = "invalid_configuration"
	// JobQueueBlockerInventoryLimit reports that connections, queues, keys, blockers, or output exceeded bounds.
	JobQueueBlockerInventoryLimit JobQueueBlockerCode = "inventory_limit"
	// JobQueueBlockerRedisUnavailable reports that Redis could not be reached or timed out during inspection.
	JobQueueBlockerRedisUnavailable JobQueueBlockerCode = "redis_unavailable"
	// JobQueueBlockerLayout1RequiresUpgrade reports a layout-1 queue that must upgrade through v0.13.0 first.
	JobQueueBlockerLayout1RequiresUpgrade JobQueueBlockerCode = "layout_1_requires_upgrade"
	// JobQueueBlockerUnsupportedLayout reports a queue with an unknown or future layout value.
	JobQueueBlockerUnsupportedLayout JobQueueBlockerCode = "unsupported_layout"
	// JobQueueBlockerQueuePolicyMismatch reports that stored queue policy metadata differs from configuration.
	JobQueueBlockerQueuePolicyMismatch JobQueueBlockerCode = "queue_policy_mismatch"
	// JobQueueBlockerInvalidQueueState reports missing/corrupt metadata, wrong Redis key types, or invalid counters.
	JobQueueBlockerInvalidQueueState JobQueueBlockerCode = "invalid_queue_state"
	// JobQueueBlockerLayoutInspectionUnsupported reports that a configured Redis job connection cannot report layouts.
	JobQueueBlockerLayoutInspectionUnsupported JobQueueBlockerCode = "layout_inspection_unsupported"
)

// JobQueueBlocker describes one global, connection-scoped, or queue-scoped cutover blocker.
type JobQueueBlocker struct {
	Code       JobQueueBlockerCode                 `json:"code"`
	Connection value.Nullable[jobs.ConnectionName] `json:"connection"`
	Queue      value.Nullable[jobs.Queue]          `json:"queue"`
}

// JobQueueInventory describes one declared logical queue on a Redis job connection.
type JobQueueInventory struct {
	Queue    jobs.Queue           `json:"queue"`
	Base     string               `json:"base"`
	Keys     []string             `json:"keys"`
	Layout   redis.JobQueueLayout `json:"layout"`
	Eligible bool                 `json:"eligible"`
}

// JobConnectionInventory describes one configured Redis job connection and its declared queues.
type JobConnectionInventory struct {
	Connection      jobs.ConnectionName  `json:"connection"`
	RedisConnection redis.ConnectionName `json:"redis_connection"`
	StorageID       string               `json:"storage_id"`
	Namespace       keyspace.Namespace   `json:"namespace"`
	Prefix          string               `json:"prefix"`
	PolicyID        string               `json:"policy_id"`
	Queues          []JobQueueInventory  `json:"queues"`
}

// JobQueueReport is the JSON envelope emitted by deploy:jobs-inspect.
type JobQueueReport struct {
	Schema          int                      `json:"schema"`
	SupportedLayout int                      `json:"supported_layout"`
	Coverage        JobQueueCoverage         `json:"coverage"`
	Connections     []JobConnectionInventory `json:"connections"`
	Blockers        []JobQueueBlocker        `json:"blockers"`
}

func emptyJobQueueReport(blockers ...JobQueueBlocker) JobQueueReport {
	outBlockers := make([]JobQueueBlocker, 0, len(blockers))
	outBlockers = append(outBlockers, blockers...)
	return JobQueueReport{
		Schema:          JobQueueInventorySchema,
		SupportedLayout: redis.JobSupportedLayout,
		Coverage:        JobQueueCoverageDeclared,
		Connections:     []JobConnectionInventory{},
		Blockers:        canonicalBlockers(outBlockers),
	}
}

func canonicalBlockers(blockers []JobQueueBlocker) []JobQueueBlocker {
	if len(blockers) == 0 {
		return []JobQueueBlocker{}
	}
	cloned := slices.Clone(blockers)
	slices.SortFunc(cloned, compareBlockers)
	return slices.Compact(cloned)
}

func compareBlockers(a, b JobQueueBlocker) int {
	aConn, aConnOK := a.Connection.Get()
	bConn, bConnOK := b.Connection.Get()
	if c := cmp.Compare(boolRank(aConnOK), boolRank(bConnOK)); c != 0 {
		return c
	}
	if c := cmp.Compare(aConn, bConn); c != 0 {
		return c
	}
	aQueue, aQueueOK := a.Queue.Get()
	bQueue, bQueueOK := b.Queue.Get()
	if c := cmp.Compare(boolRank(aQueueOK), boolRank(bQueueOK)); c != 0 {
		return c
	}
	if c := cmp.Compare(aQueue, bQueue); c != 0 {
		return c
	}
	return cmp.Compare(a.Code, b.Code)
}

func boolRank(ok bool) int {
	if ok {
		return 1
	}
	return 0
}

func hasBlockerCode(blockers []JobQueueBlocker, code JobQueueBlockerCode) bool {
	for _, b := range blockers {
		if b.Code == code {
			return true
		}
	}
	return false
}

func (b *Builder) jobSnapshot() (Settings, options, []JobDeclaration, []EventDeclaration, []foundation.Provider, []foundation.Plugin, []slots.Declaration, error) {
	if b == nil || b.state == nil {
		return Settings{}, options{}, nil, nil, nil, nil, nil, safeerror.NewFault(fault.Invalid, "application builder is required")
	}
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if b.state.built {
		return Settings{}, options{}, nil, nil, nil, nil, nil, safeerror.NewFault(fault.Closed, "application builder is already built")
	}
	return b.state.settings, b.state.options, slices.Clone(b.state.jobs), slices.Clone(b.state.events), slices.Clone(b.state.providers), slices.Clone(b.state.plugins), slices.Clone(b.state.models), b.state.err
}

type declaredJobKey struct {
	connection jobs.ConnectionName
	name       jobs.Name
	version    jobs.Version
}

func (b *Builder) jobQueuesWithPlan() (JobQueueReport, *infrastructure.Plan, error) {
	s, configured, jobDeclarations, eventDeclarations, providers, plugins, models, err := b.jobSnapshot()
	if err != nil {
		report := emptyJobQueueReport(JobQueueBlocker{Code: JobQueueBlockerInvalidConfiguration})
		return report, nil, err
	}
	blockers := make([]JobQueueBlocker, 0)
	if len(providers) > 0 || len(plugins) > 0 {
		blockers = append(blockers, JobQueueBlocker{Code: JobQueueBlockerOpaqueProvider})
	}
	failConfig := func(cause error) (JobQueueReport, *infrastructure.Plan, error) {
		blockers = append(blockers, JobQueueBlocker{Code: JobQueueBlockerInvalidConfiguration})
		return emptyJobQueueReport(blockers...), nil, cause
	}
	if len(jobDeclarations) > 4096 || len(eventDeclarations) > 4096 {
		return failConfig(safeerror.NewFault(fault.Invalid, "too many application declarations"))
	}
	if err := validateModelExtensions(s.Features, models); err != nil {
		return failConfig(err)
	}
	eventOutboxes, err := prepareEventOutboxes(s, eventDeclarations)
	if err != nil {
		return failConfig(err)
	}
	if len(eventDeclarations) > 0 && !s.Features.Events.Enabled {
		return failConfig(safeerror.NewFault(fault.Invalid, "event declarations require events to be enabled"))
	}
	for _, d := range eventDeclarations {
		if d.install == nil || d.err != nil {
			if d.err != nil {
				return failConfig(d.err)
			}
			return failConfig(safeerror.NewFault(fault.Invalid, "invalid event declaration"))
		}
		if d.queued && !s.Features.Events.QueuedListeners {
			return failConfig(safeerror.NewFault(fault.Invalid, "queued event listeners require features.events.queued_listeners"))
		}
	}
	s.Features, err = prepareFeatureSettings(s, configured.clock)
	if err != nil {
		return failConfig(err)
	}
	if name := s.Features.ExtensionCleanup.Jobs; name != "" {
		if conn, ok := s.Services.Jobs.Connections[name]; !ok || conn.Driver != infrastructure.RedisJobs {
			return failConfig(safeerror.NewFault(fault.Invalid, "extension cleanup requires a durable Redis job connection"))
		}
		jobDeclarations = append(jobDeclarations, extensionCleanupDeclaration(name))
	}
	if s.Features.Outbox.Enabled {
		for _, name := range slices.Sorted(maps.Keys(s.Features.Outbox.Jobs)) {
			if conn, ok := s.Services.Jobs.Connections[name]; !ok || conn.Driver != infrastructure.RedisJobs {
				return failConfig(safeerror.NewFault(fault.Invalid, "transactional job outbox requires a durable Redis job connection"))
			}
		}
	}
	if s.Worker.Archive, err = prepareJobArchive(s); err != nil {
		return failConfig(err)
	}
	plan, err := infrastructure.ConfigureJobs(s.Services, append(slices.Clone(configured.infrastructure), infrastructure.WithClock(configured.clock))...)
	if err != nil {
		return failConfig(err)
	}
	jobsSettings := plan.Jobs()
	declaredQueues := make(map[jobs.ConnectionName]map[jobs.Queue]struct{}, len(jobsSettings.Connections))
	addQueue := func(conn jobs.ConnectionName, queue jobs.Queue) error {
		if err := queue.Validate(); err != nil {
			return err
		}
		set := declaredQueues[conn]
		if set == nil {
			set = make(map[jobs.Queue]struct{})
			declaredQueues[conn] = set
		}
		set[queue] = struct{}{}
		return nil
	}
	for _, connName := range slices.Sorted(maps.Keys(jobsSettings.Connections)) {
		conn := jobsSettings.Connections[connName]
		if err := addQueue(connName, conn.DefaultQueue); err != nil {
			return failConfig(err)
		}
	}
	workerConfigured := s.Worker.Enabled || s.Worker.Connection != "" || len(s.Worker.Config.Queues) > 0 || s.Worker.Config.Namespace != (keyspace.Namespace{})
	if workerConfigured || len(jobsSettings.Connections) > 0 {
		if _, err := plan.Worker(WorkerProvider, s.Worker.Connection, s.Worker.Config, nil); err != nil {
			return failConfig(err)
		}
		workerConn := s.Worker.Connection
		if workerConn == "" {
			workerConn = jobsSettings.Default
		}
		conn := jobsSettings.Connections[workerConn]
		if len(s.Worker.Config.Queues) == 0 {
			if err := addQueue(workerConn, conn.DefaultQueue); err != nil {
				return failConfig(err)
			}
		} else {
			for _, sub := range s.Worker.Config.Queues {
				if err := addQueue(workerConn, sub.Queue); err != nil {
					return failConfig(err)
				}
			}
		}
	}
	seenJobs := make(map[declaredJobKey]struct{}, len(jobDeclarations)+len(eventOutboxes)+1)
	recordJob := func(conn jobs.ConnectionName, name jobs.Name, version jobs.Version, queue jobs.Queue) error {
		key := declaredJobKey{connection: conn, name: name, version: version}
		if _, exists := seenJobs[key]; exists {
			return safeerror.NewFault(fault.Duplicate, "duplicate job declaration")
		}
		seenJobs[key] = struct{}{}
		return addQueue(conn, queue)
	}
	for _, d := range jobDeclarations {
		if d.install == nil || d.err != nil {
			if d.err != nil {
				return failConfig(d.err)
			}
			return failConfig(safeerror.NewFault(fault.Invalid, "invalid job declaration"))
		}
		connName := d.connection
		if connName == "" {
			connName = jobsSettings.Default
		}
		conn, ok := jobsSettings.Connections[connName]
		if !ok {
			return failConfig(safeerror.NewFault(fault.Missing, "job declaration connection is not configured"))
		}
		queue := d.policy.Queue
		if queue == "" {
			queue = conn.DefaultQueue
		}
		if err := recordJob(connName, d.name, d.version, queue); err != nil {
			return failConfig(err)
		}
	}
	if s.Features.Events.Enabled && s.Features.Events.QueuedListeners {
		connName := s.Features.Events.ListenerConnection
		queue := s.Features.Events.ListenerQueue
		listenerDef := events.ListenerDefinition(jobs.DefaultPolicy(queue))
		if err := recordJob(connName, listenerDef.Name(), listenerDef.Version(), queue); err != nil {
			return failConfig(err)
		}
	}
	for _, group := range eventOutboxes {
		conn, ok := jobsSettings.Connections[group.connection]
		if !ok || conn.Driver != infrastructure.RedisJobs {
			return failConfig(safeerror.NewFault(fault.Invalid, "event outbox requires a durable Redis job connection"))
		}
		outboxDef := events.QueuedOutboxDefinition(group.policy)
		if err := recordJob(group.connection, outboxDef.Name(), outboxDef.Version(), group.policy.Queue); err != nil {
			return failConfig(err)
		}
	}
	connections := make([]JobConnectionInventory, 0)
	totalQueues := 0
	for _, connName := range slices.Sorted(maps.Keys(jobsSettings.Connections)) {
		conn := jobsSettings.Connections[connName]
		if conn.Driver != infrastructure.RedisJobs {
			continue
		}
		storageID, err := plan.RedisStorageID(conn.Redis)
		if err != nil {
			return failConfig(err)
		}
		policyID, err := redis.JobPolicyID(conn.Queue)
		if err != nil {
			return failConfig(err)
		}
		prefix := keyaddress.Prefix(jobs.KeyFeature, conn.Dispatch.Namespace)
		queueNames := slices.Sorted(maps.Keys(declaredQueues[connName]))
		queues := make([]JobQueueInventory, 0, len(queueNames))
		for _, qName := range queueNames {
			key, err := jobs.NewKey(conn.Dispatch.Namespace, qName)
			if err != nil {
				return failConfig(err)
			}
			physKeys, err := redis.JobKeys(key)
			if err != nil {
				return failConfig(err)
			}
			if len(physKeys) > MaxJobQueueKeys {
				blockers = append(blockers, JobQueueBlocker{Code: JobQueueBlockerInventoryLimit})
				return emptyJobQueueReport(blockers...), nil, safeerror.NewFault(fault.Invalid, "job queue key inventory exceeds maximum bound")
			}
			queues = append(queues, JobQueueInventory{
				Queue:    qName,
				Base:     key.String(),
				Keys:     physKeys,
				Layout:   redis.JobQueueLayoutUnknown,
				Eligible: false,
			})
		}
		totalQueues += len(queues)
		connections = append(connections, JobConnectionInventory{
			Connection:      connName,
			RedisConnection: conn.Redis,
			StorageID:       storageID,
			Namespace:       conn.Dispatch.Namespace,
			Prefix:          prefix,
			PolicyID:        policyID,
			Queues:          queues,
		})
	}
	if len(connections) > MaxJobQueueConnections || totalQueues > MaxJobQueues {
		blockers = append(blockers, JobQueueBlocker{Code: JobQueueBlockerInventoryLimit})
		return emptyJobQueueReport(blockers...), nil, safeerror.NewFault(fault.Invalid, "job queue inventory exceeds maximum bounds")
	}
	report := JobQueueReport{
		Schema:          JobQueueInventorySchema,
		SupportedLayout: redis.JobSupportedLayout,
		Coverage:        JobQueueCoverageDeclared,
		Connections:     connections,
		Blockers:        canonicalBlockers(blockers),
	}
	if len(report.Blockers) > MaxJobQueueBlockers {
		limitReport := emptyJobQueueReport(JobQueueBlocker{Code: JobQueueBlockerInventoryLimit})
		return limitReport, nil, safeerror.NewFault(fault.Invalid, "job queue blockers exceed maximum bound")
	}
	if len(report.Blockers) > 0 {
		return report, plan, safeerror.NewFault(fault.Conflict, "job queue inventory reported cutover blockers")
	}
	return report, plan, nil
}

// JobQueues derives the configured Redis job connections, declared logical queues,
// physical key sets, storage/policy digests, and static completeness blockers
// from the Builder declaration graph without connecting to Redis or constructing
// business job handlers. Use InspectJobQueues to populate live Redis layouts.
func (b *Builder) JobQueues() (JobQueueReport, error) {
	report, _, err := b.jobQueuesWithPlan()
	return cloneJobQueueReport(report), err
}

// InspectJobQueues derives the declared Redis job queue inventory from builder
// and performs bounded read-only Redis layout inspection on every declared queue
// without starting kernels or constructing business job handlers.
func InspectJobQueues(ctx context.Context, builder *Builder) (JobQueueReport, error) {
	if ctx == nil {
		report := emptyJobQueueReport(JobQueueBlocker{Code: JobQueueBlockerInvalidConfiguration})
		return report, safeerror.NewFault(fault.Invalid, "job queue inspection requires a context")
	}
	report, plan, err := builder.jobQueuesWithPlan()
	if plan == nil || hasBlockerCode(report.Blockers, JobQueueBlockerInvalidConfiguration) || hasBlockerCode(report.Blockers, JobQueueBlockerInventoryLimit) {
		return finalizeJobQueueReport(report, err)
	}
	if len(report.Connections) > 0 {
		targets := make(map[jobs.ConnectionName][]jobs.Queue, len(report.Connections))
		for _, conn := range report.Connections {
			queues := make([]jobs.Queue, 0, len(conn.Queues))
			for _, q := range conn.Queues {
				queues = append(queues, q.Queue)
			}
			targets[conn.Connection] = queues
		}
		outcomes := plan.InspectJobLayouts(ctx, targets)
		for i := range report.Connections {
			conn := &report.Connections[i]
			outcome, ok := outcomes[conn.Connection]
			if !ok {
				for j := range conn.Queues {
					conn.Queues[j].Layout = redis.JobQueueLayoutUnknown
					conn.Queues[j].Eligible = false
				}
				report.Blockers = append(report.Blockers, JobQueueBlocker{
					Code:       JobQueueBlockerLayoutInspectionUnsupported,
					Connection: value.Of(conn.Connection),
				})
				continue
			}
			if outcome.ConnectionError != nil {
				for j := range conn.Queues {
					conn.Queues[j].Layout = redis.JobQueueLayoutUnknown
					conn.Queues[j].Eligible = false
				}
				report.Blockers = append(report.Blockers, JobQueueBlocker{
					Code:       classifyConnectionInspectionError(outcome.ConnectionError),
					Connection: value.Of(conn.Connection),
				})
				continue
			}
			for j := range conn.Queues {
				q := &conn.Queues[j]
				if qErr, hasErr := outcome.QueueErrors[q.Queue]; hasErr && qErr != nil {
					q.Layout = redis.JobQueueLayoutUnknown
					q.Eligible = false
					report.Blockers = append(report.Blockers, JobQueueBlocker{
						Code:       classifyQueueInspectionError(qErr),
						Connection: value.Of(conn.Connection),
						Queue:      value.Of(q.Queue),
					})
					continue
				}
				insp, hasInsp := outcome.Queues[q.Queue]
				if !hasInsp {
					q.Layout = redis.JobQueueLayoutUnknown
					q.Eligible = false
					report.Blockers = append(report.Blockers, JobQueueBlocker{
						Code:       JobQueueBlockerLayoutInspectionUnsupported,
						Connection: value.Of(conn.Connection),
						Queue:      value.Of(q.Queue),
					})
					continue
				}
				q.Layout = insp.Layout
				q.Eligible = insp.Eligible
				if !insp.Eligible {
					code := JobQueueBlockerCode(insp.Blocker)
					if code == "" {
						code = JobQueueBlockerInvalidQueueState
					}
					report.Blockers = append(report.Blockers, JobQueueBlocker{
						Code:       code,
						Connection: value.Of(conn.Connection),
						Queue:      value.Of(q.Queue),
					})
				}
			}
		}
	}
	return finalizeJobQueueReport(report, err)
}

func finalizeJobQueueReport(report JobQueueReport, existingErr error) (JobQueueReport, error) {
	if report.Connections == nil {
		report.Connections = []JobConnectionInventory{}
	}
	report.Blockers = canonicalBlockers(report.Blockers)
	if len(report.Blockers) > MaxJobQueueBlockers {
		limitReport := emptyJobQueueReport(JobQueueBlocker{Code: JobQueueBlockerInventoryLimit})
		return limitReport, safeerror.NewFault(fault.Conflict, "job queue blockers exceed maximum bound")
	}
	if len(report.Blockers) > 0 {
		if existingErr != nil {
			return cloneJobQueueReport(report), existingErr
		}
		return cloneJobQueueReport(report), safeerror.NewFault(fault.Conflict, "job queue inventory reported cutover blockers")
	}
	return cloneJobQueueReport(report), nil
}

func cloneJobQueueReport(report JobQueueReport) JobQueueReport {
	connections := make([]JobConnectionInventory, len(report.Connections))
	for i, c := range report.Connections {
		queues := make([]JobQueueInventory, len(c.Queues))
		for j, q := range c.Queues {
			q.Keys = slices.Clone(q.Keys)
			queues[j] = q
		}
		c.Queues = queues
		connections[i] = c
	}
	report.Connections = connections
	report.Blockers = slices.Clone(report.Blockers)
	if report.Blockers == nil {
		report.Blockers = []JobQueueBlocker{}
	}
	return report
}

func classifyConnectionInspectionError(err error) JobQueueBlockerCode {
	if callback.ErrorIs(err, fault.Invalid) || callback.ErrorIs(err, fault.Missing) {
		return JobQueueBlockerInvalidConfiguration
	}
	return JobQueueBlockerRedisUnavailable
}

func classifyQueueInspectionError(err error) JobQueueBlockerCode {
	if callback.ErrorIs(err, fault.Timeout) || callback.ErrorIs(err, fault.Closed) || callback.ErrorIs(err, fault.Overloaded) ||
		callback.ErrorIs(err, context.Canceled) || callback.ErrorIs(err, context.DeadlineExceeded) || callback.ErrorIs(err, io.EOF) {
		return JobQueueBlockerRedisUnavailable
	}
	var netErr net.Error
	if callback.ErrorAs(err, &netErr) {
		return JobQueueBlockerRedisUnavailable
	}
	return JobQueueBlockerLayoutInspectionUnsupported
}

func encodeJobQueueReport(report JobQueueReport, format inspection.Format) ([]byte, bool, error) {
	if format != inspection.Text && format != inspection.JSON {
		return nil, false, safeerror.NewFault(fault.Invalid, "unsupported job queue inventory output format")
	}
	render := func(r JobQueueReport) ([]byte, error) {
		var buf bytes.Buffer
		if format == inspection.JSON {
			encoder := json.NewEncoder(&buf)
			encoder.SetEscapeHTML(false)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(r); err != nil {
				return nil, err
			}
			return buf.Bytes(), nil
		}
		for _, conn := range r.Connections {
			for _, q := range conn.Queues {
				if _, err := fmt.Fprintf(&buf, "connection\t%s\tredis\t%s\tqueue\t%s\tlayout\t%s\teligible\t%t\tbase\t%s\n",
					conn.Connection, conn.RedisConnection, q.Queue, q.Layout, q.Eligible, q.Base); err != nil {
					return nil, err
				}
			}
		}
		for _, blocker := range r.Blockers {
			connName, _ := blocker.Connection.Get()
			queueName, _ := blocker.Queue.Get()
			if _, err := fmt.Fprintf(&buf, "blocker\t%s\tconnection\t%s\tqueue\t%s\n",
				blocker.Code, connName, queueName); err != nil {
				return nil, err
			}
		}
		return buf.Bytes(), nil
	}
	encoded, err := render(report)
	if err != nil {
		return nil, false, err
	}
	if len(encoded) > MaxJobQueueInventoryBytes {
		limitReport := emptyJobQueueReport(JobQueueBlocker{Code: JobQueueBlockerInventoryLimit})
		encoded, err = render(limitReport)
		if err != nil {
			return nil, true, err
		}
		return encoded, true, nil
	}
	return encoded, false, nil
}

// WriteJobQueues formats a job queue inventory report as text or JSON, enforcing
// the 4 MiB output ceiling with a fail-closed inventory_limit blocker.
func WriteJobQueues(w io.Writer, format inspection.Format, report JobQueueReport) error {
	if w == nil {
		return safeerror.NewFault(fault.Invalid, "job queue inventory writer is nil")
	}
	if report.Schema == 0 {
		report.Schema = JobQueueInventorySchema
	}
	if report.SupportedLayout == 0 {
		report.SupportedLayout = redis.JobSupportedLayout
	}
	if report.Coverage == "" {
		report.Coverage = JobQueueCoverageDeclared
	}
	if report.Connections == nil {
		report.Connections = []JobConnectionInventory{}
	}
	report.Blockers = canonicalBlockers(report.Blockers)
	encoded, exceeded, err := encodeJobQueueReport(report, format)
	if err != nil {
		return err
	}
	if _, err := w.Write(encoded); err != nil {
		return err
	}
	if exceeded {
		return safeerror.NewFault(fault.Conflict, "job queue inventory output exceeds maximum size")
	}
	return nil
}

// ProbeJobQueues inspects configured Redis job queues and writes the JSON
// report to output, returning a non-nil error if any blocker is present.
func ProbeJobQueues(ctx context.Context, builder *Builder, output io.Writer) error {
	if ctx == nil || builder == nil || output == nil {
		return safeerror.NewFault(fault.Invalid, "job queue probe requires a context, builder and output writer")
	}
	report, inspectErr := InspectJobQueues(ctx, builder)
	writeErr := WriteJobQueues(output, inspection.JSON, report)
	if writeErr != nil {
		return writeErr
	}
	return inspectErr
}

// JobQueuesCommand declares the release-owned job queue inventory CLI command
// (conventionally named "deploy:jobs-inspect"). It runs without Builder.Build,
// HTTP/worker/scheduler/outbox startup, or business job handler construction.
func JobQueuesCommand(name cli.Name, builder *Builder) (cli.Declaration, error) {
	if builder == nil {
		return cli.Declaration{}, safeerror.NewFault(fault.Invalid, "job queue inventory command requires an application builder")
	}
	return cli.Define(name, "Inspect configured Redis job queues and storage layouts for cutover", decodeJobQueuesFormat).AllowDuringMaintenance().Declare(func(foundation.Resolver) (cli.Handler[inspection.Format], error) {
		return func(ctx context.Context, format inspection.Format, streams cli.Streams) error {
			report, inspectErr := InspectJobQueues(ctx, builder)
			if writeErr := WriteJobQueues(streams.Out, format, report); writeErr != nil {
				return writeErr
			}
			if inspectErr != nil {
				return safeerror.NewFault(fault.Conflict, "job queue inventory reported cutover blockers")
			}
			return nil
		}, nil
	})
}

func decodeJobQueuesFormat(args []string, help io.Writer) (inspection.Format, error) {
	flags := flag.NewFlagSet("", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", string(inspection.Text), "output format: text or json")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(help, "Flags:")
		flags.SetOutput(help)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", flag.ErrHelp
		}
		return "", clidiag.Usage("invalid job queue inventory flags; use --help")
	}
	if flags.NArg() != 0 {
		return "", clidiag.Usage("job queue inventory accepts no positional arguments")
	}
	selected := inspection.Format(*format)
	if selected != inspection.Text && selected != inspection.JSON {
		return "", clidiag.Usage("unsupported job queue inventory format; use text or json")
	}
	return selected, nil
}

// RunJobCommand executes one jobs/command operation (such as migrate-layout)
// against builder by starting only the configured job connections and their
// Redis pools, without starting HTTP, workers, schedulers, outbox relays,
// databases, or business job handlers.
func RunJobCommand(ctx context.Context, builder *Builder, command jobcommand.Command, output io.Writer) (err error) {
	if ctx == nil || builder == nil || output == nil {
		return safeerror.NewFault(fault.Invalid, "job command requires a context, builder and output writer")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s, configured, _, _, _, _, _, err := builder.jobSnapshot()
	if err != nil {
		return err
	}
	if s.TimeZone == "" {
		s.TimeZone = temporal.UTC
	}
	if err := validateShutdownBudget(s); err != nil {
		return err
	}
	channels, err := logging.PrepareChannels(s.Log.Default, s.Log.inTimeZone(s.TimeZone), configured.logger, configured.logHandlers...)
	if err != nil {
		return err
	}
	logger, err := channels.Channels().Default()
	if err != nil {
		return err
	}
	plan, err := infrastructure.ConfigureJobs(s.Services, append(slices.Clone(configured.infrastructure), infrastructure.WithClock(configured.clock), infrastructure.WithLogger(logger))...)
	if err != nil {
		return err
	}
	fBuilder := foundation.NewBuilder(
		foundation.WithClock(configured.clock),
		foundation.WithLogger(logger),
		foundation.WithShutdownTimeout(s.ShutdownTimeout),
		foundation.WithStopDelay(s.StopDelay),
		foundation.WithStartupTimeout(s.StartupTimeout),
	)
	fBuilder.Register(foundation.Module{Name: "foundry.application.logging", OnBoot: func(ctx context.Context, r *foundation.Runtime) error {
		if err := r.OnShutdown("channels", func(context.Context) error { return channels.Close() }); err != nil {
			return safeerror.Join("operation failed", err, channels.Close())
		}
		return channels.Start(ctx)
	}})
	plan.Register(fBuilder)
	app, err := fBuilder.Build(ctx)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), app.ShutdownTimeout())
		defer cancel()
		err = safeerror.Join("operation failed", err, app.Shutdown(shutdownCtx))
	}()
	if err := app.Start(ctx); err != nil {
		return err
	}
	resources, err := foundation.Resolve(app.Services(), infrastructure.ServicesKey)
	if err != nil {
		return err
	}
	return command.Run(ctx, resources.Jobs, output)
}

// JobCommand declares a restricted "jobs" CLI command bound to builder. Unlike
// BuiltInCommands, it starts only configured job connections and their Redis
// pools when invoked, without booting HTTP, workers, schedulers, outbox relays,
// databases, or business job handlers.
func JobCommand(builder *Builder) (cli.Declaration, error) {
	if builder == nil {
		return cli.Declaration{}, safeerror.NewFault(fault.Invalid, "job command requires an application builder")
	}
	return cli.Define("jobs", "Inspect, retry and manage retained jobs", func(args []string, help io.Writer) (jobcommand.Command, error) {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return jobcommand.Parse(args, help)
		}
		return jobcommand.Parse(append([]string{"jobs"}, args...), help)
	}).AllowDuringMaintenance().Declare(func(foundation.Resolver) (cli.Handler[jobcommand.Command], error) {
		return func(ctx context.Context, command jobcommand.Command, streams cli.Streams) error {
			return RunJobCommand(ctx, builder, command, streams.Out)
		}, nil
	})
}

// DeployJobCommands returns the restricted CLI declarations for release-owned
// job queue inspection ("deploy:jobs-inspect") and queue operations ("jobs",
// including "jobs migrate-layout") bound to builder. Neither command starts
// HTTP, workers, schedulers, outbox relays, databases, or business job handlers.
func DeployJobCommands(builder *Builder) ([]cli.Declaration, error) {
	inspectCmd, err := JobQueuesCommand("deploy:jobs-inspect", builder)
	if err != nil {
		return nil, err
	}
	jobsCmd, err := JobCommand(builder)
	if err != nil {
		return nil, err
	}
	return []cli.Declaration{inspectCmd, jobsCmd}, nil
}
