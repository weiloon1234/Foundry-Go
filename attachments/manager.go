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

type Config struct {
	MaxActive      int
	Timeout        time.Duration
	CleanupTimeout time.Duration
	StoredGrace    time.Duration
}

func DefaultConfig() Config {
	return Config{MaxActive: 2, Timeout: 2 * time.Minute, CleanupTimeout: 10 * time.Second, StoredGrace: 5 * time.Minute}
}
func (c Config) Validate() error {
	if c.MaxActive < 1 || c.MaxActive > 64 || c.Timeout <= 0 || c.Timeout > 30*time.Minute || c.CleanupTimeout <= 0 || c.CleanupTimeout > time.Minute || c.StoredGrace < time.Minute || c.StoredGrace > 24*time.Hour {
		return invalid()
	}
	return nil
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
	writer      model.ID[store.Writer]
	mu          sync.Mutex
	writing     map[model.ID[store.File]]bool
}

func New(dependencies Dependencies, config Config, collections ...Registration) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if err := dependencies.Store.Validate(); err != nil {
		return nil, err
	}
	if dependencies.Disks == nil || len(collections) > 4096 {
		return nil, invalid()
	}
	m := &Manager{store: dependencies.Store, disks: dependencies.Disks, image: dependencies.Image, locales: dependencies.Locales, config: config, collections: make(map[string]Registration, len(collections)), writing: make(map[model.ID[store.File]]bool)}
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
		if collection.policy.Image.IsSet() {
			if err := dependencies.Image.Validate(); err != nil {
				return nil, err
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
	m.calls, err = workscope.New(config.MaxActive, config.Timeout)
	if err != nil {
		return nil, err
	}
	return m, nil
}
func (m *Manager) Validate() error {
	if m == nil || m.calls == nil || m.collections == nil || m.writer.IsZero() {
		return invalid()
	}
	return m.store.Validate()
}
func (m *Manager) Close(ctx context.Context) error {
	if err := m.Validate(); err != nil {
		return err
	}
	return m.calls.Close(ctx)
}
func (m *Manager) Done() <-chan struct{} {
	if m == nil || m.calls == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return m.calls.Done()
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
