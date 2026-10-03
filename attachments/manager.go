package attachments

import (
	"context"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/imaging"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Config bounds manager work. MaxActive admits uploads and other mutations;
// MaxReads separately admits loads, lookups, reads and link signing, so a burst
// of reads cannot starve writes (or the reverse). Both queue briefly (at most
// min(Timeout, 5s)) before failing as retryable overload. Owner-delete cleanup
// joined from model observers uses its own MaxOwnerCleanup pool so it does not
// compete with application reads/writes. SettleAfter is added to a disk's
// operation Timeout before ReconcilePending may inspect an unresolved write.
// Zero MaxReads or SettleAfter selects its default.
type Config struct {
	MaxActive      int
	MaxReads       int
	Timeout        time.Duration
	CleanupTimeout time.Duration
	StoredGrace    time.Duration
	SettleAfter    time.Duration
}

const (
	DefaultMaxReads    = 64
	DefaultSettleAfter = 15 * time.Minute
	// MaxOwnerCleanup bounds concurrent owner-delete observers per manager.
	MaxOwnerCleanup = 4096
)

func DefaultConfig() Config {
	return Config{MaxActive: 8, MaxReads: DefaultMaxReads, Timeout: 2 * time.Minute, CleanupTimeout: 10 * time.Second, StoredGrace: 5 * time.Minute, SettleAfter: DefaultSettleAfter}
}
func (c Config) Validate() error {
	if c.MaxActive < 1 || c.MaxActive > 256 || c.MaxReads < 0 || c.MaxReads > 4096 || c.Timeout <= 0 || c.Timeout > 30*time.Minute || c.CleanupTimeout <= 0 || c.CleanupTimeout > time.Minute || c.StoredGrace < time.Minute || c.StoredGrace > 24*time.Hour || c.SettleAfter < 0 || c.SettleAfter != 0 && c.SettleAfter < time.Minute || c.SettleAfter > 7*24*time.Hour {
		return invalid()
	}
	return nil
}
func (c Config) normalized() Config {
	if c.MaxReads == 0 {
		c.MaxReads = DefaultMaxReads
	}
	if c.SettleAfter == 0 {
		c.SettleAfter = DefaultSettleAfter
	}
	return c
}

// Dependencies are borrowed. Image and Locales are required only by collections
// that use them. Their owners must outlive this manager's actual operations.
type Dependencies struct {
	Store   *extensions.Store
	Disks   *storage.Registry
	Image   *imaging.Engine
	Locales i18n.LocaleCatalog
}
type Manager struct {
	store       *extensions.Store
	disks       *storage.Registry
	image       *imaging.Engine
	locales     i18n.LocaleCatalog
	config      Config
	collections map[string]Registration
	calls       *workscope.Group
	reads       *workscope.Group
	owners      *workscope.Group
	closeOnce   sync.Once
	done        chan struct{}
	writer      model.ID[store.Writer]
	mu          sync.Mutex
	writing     map[model.ID[store.File]]bool
	// variantWriting excludes locally active variant writers from sweeps.
	variantWriting map[model.ID[store.Variant]]bool
}

func New(dependencies Dependencies, config Config, collections ...Registration) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	config = config.normalized()
	if err := dependencies.Store.Validate(); err != nil {
		return nil, err
	}
	if dependencies.Disks == nil || len(collections) > 4096 {
		return nil, invalid()
	}
	m := &Manager{store: dependencies.Store, disks: dependencies.Disks, image: dependencies.Image, locales: dependencies.Locales, config: config, collections: make(map[string]Registration, len(collections)), writing: make(map[model.ID[store.File]]bool), variantWriting: make(map[model.ID[store.Variant]]bool), done: make(chan struct{})}
	for _, collection := range collections {
		if collection.id == nil || collection.validate == nil {
			return nil, invalid()
		}
		if err := collection.validate(dependencies.Store.Registry()); err != nil {
			return nil, err
		}
		if _, ok := m.collections[collection.key]; ok {
			return nil, fault.New(fault.Duplicate, "attachment collection already registered")
		}
		disk, err := collection.policy.Disk.Resolve(dependencies.Disks)
		if err != nil {
			return nil, err
		}
		capabilities := disk.Capabilities()
		if !capabilities.ConditionalCreate || !capabilities.ConditionalRead || !capabilities.ConditionalDelete {
			return nil, storage.Failure(storage.Unsupported, storage.PutOperation, storage.Unchanged, nil)
		}
		if err := capabilities.ValidatePut(storage.PutOptions{Size: value.Set(collection.policy.MaxStoredBytes), Condition: storage.IfAbsent()}); err != nil {
			return nil, err
		}
		if collection.policy.Image.IsSet() || len(collection.policy.Variants) > 0 {
			if err := dependencies.Image.Validate(); err != nil {
				return nil, err
			}
			if plan, set := collection.policy.Image.Get(); set {
				if err := dependencies.Image.ValidatePlan(plan); err != nil {
					return nil, err
				}
			}
			for _, variant := range collection.policy.Variants {
				if err := dependencies.Image.ValidatePlan(variant.plan); err != nil {
					return nil, err
				}
			}
			if !collection.policy.AnyMedia {
				for _, media := range collection.policy.Accepted {
					format, err := imaging.ParseMediaType(string(media))
					if err != nil {
						return nil, invalid()
					}
					capability, ok := dependencies.Image.Capabilities().ForFormat(format)
					if !ok || !capability.Read {
						return nil, invalid()
					}
				}
			}
		}
		if collection.policy.Localized {
			if err := i18n.ValidateLocaleCatalog(dependencies.Locales); err != nil {
				return nil, err
			}
		}
		m.collections[collection.key] = collection
	}
	var err error
	m.writer, err = model.NewID[store.Writer]()
	if err != nil {
		return nil, err
	}
	if m.calls, err = workscope.New(config.MaxActive, config.Timeout); err != nil {
		return nil, err
	}
	if m.reads, err = workscope.New(config.MaxReads, config.Timeout); err != nil {
		return nil, err
	}
	if m.owners, err = workscope.New(MaxOwnerCleanup, config.Timeout); err != nil {
		return nil, err
	}
	return m, nil
}
func (m *Manager) Validate() error {
	if m == nil || m.calls == nil || m.reads == nil || m.owners == nil || m.done == nil || m.collections == nil || m.writer.IsZero() {
		return invalid()
	}
	return m.store.Validate()
}

// Close cancels reads, writes and owner cleanup together and waits for their
// actual exit. A timed-out wait keeps ownership visible through Done.
func (m *Manager) Close(ctx context.Context) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return invalid()
	}
	groups := []*workscope.Group{m.calls, m.reads, m.owners}
	for _, group := range groups {
		if err := group.CheckClose(ctx); err != nil {
			return err
		}
	}
	m.closeOnce.Do(func() {
		// An already-ended wait starts every group's shutdown without waiting
		// for one pool to drain before cancelling the next.
		initiate, cancel := context.WithCancel(context.WithoutCancel(ctx))
		cancel()
		for _, group := range groups {
			_ = group.Close(initiate)
		}
		go func() {
			for _, group := range groups {
				<-group.Done()
			}
			close(m.done)
		}()
	})
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) Done() <-chan struct{} {
	if m == nil || m.done == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return m.done
}
func (m *Manager) markWriting(id model.ID[store.File]) func() {
	m.mu.Lock()
	m.writing[id] = true
	m.mu.Unlock()
	return func() { m.mu.Lock(); delete(m.writing, id); m.mu.Unlock() }
}
func (m *Manager) isWriting(id model.ID[store.File]) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writing[id]
}
