// Package scaffold distributes typed plugin scaffold renderers. Renderers use
// ordinary Go functions and produce files through the same ownership/protection
// rules as plugin assets. They never execute automatically during boot.
package scaffold

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/plugin/assets"
	"github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

type ID string
type Report = assets.Report

// Scaffold preserves concrete input T. The synchronous renderer must cooperate
// with context and finish before returning; panic and Goexit become errors.
// Foundry never abandons a running callback or writes a partial rendered set.
type Scaffold[T any] struct {
	info   manifest.Distribution
	render func(context.Context, T) ([]assets.File, error)
}

func New[T any](declaration manifest.Manifest, id ID, render func(context.Context, T) ([]assets.File, error)) (*Scaffold[T], error) {
	if err := declaration.Validate(); err != nil {
		return nil, err
	}
	info := manifest.Distribution{Plugin: declaration.ID, Release: declaration.Version, Kind: manifest.Scaffold, Name: string(id)}
	if err := info.Validate(); err != nil {
		return nil, err
	}
	if render == nil {
		return nil, fault.New(fault.Invalid, "plugin scaffold requires a renderer")
	}
	return &Scaffold[T]{info, render}, nil
}

func (s *Scaffold[T]) Info() manifest.Distribution {
	if s == nil {
		return manifest.Distribution{}
	}
	return s.info
}

// Render returns validated caller-owned bytes without touching the filesystem.
func (s *Scaffold[T]) Render(ctx context.Context, input T) (map[string][]byte, error) {
	if s == nil || s.render == nil || ctx == nil {
		return nil, fault.New(fault.Invalid, "invalid plugin scaffold or context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result map[string][]byte
	err := callback.Isolated("render plugin scaffold", func() error {
		files, err := s.render(ctx, input)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return fault.New(fault.Invalid, "plugin scaffold rendered no files")
		}
		result, err = assets.Snapshot(files)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Scaffold[T]) Publish(ctx context.Context, dir string, input T, check bool) (Report, error) {
	files, err := s.Render(ctx, input)
	if err != nil {
		return Report{}, err
	}
	return generate.PublishDistribution(ctx, dir, s.info, files, check)
}

var declarations = foundation.NewCollection[manifest.Distribution]("plugin.scaffolds")

func scaffoldKey[T any](owner manifest.ID, id ID) foundation.Key[*Scaffold[T]] {
	return foundation.NewKey[*Scaffold[T]](fmt.Sprintf("plugin.scaffold.%q.%q", owner, id))
}

// Register retains both the concrete renderer and value-free metadata. Its
// declared input type remains part of duplicate/override validation.
func Register[T any](r *foundation.Registrar, scaffold *Scaffold[T]) error {
	declaration, ok := r.Plugin()
	if !ok || scaffold == nil || scaffold.render == nil || scaffold.info.Plugin != declaration.ID || scaffold.info.Release != declaration.Version {
		return fault.New(fault.Invalid, "scaffold differs from its plugin owner or release")
	}
	if err := foundation.Provide(r, scaffoldKey[T](declaration.ID, ID(scaffold.info.Name)), scaffold); err != nil {
		return err
	}
	return foundation.ContributeAs[T](r, declarations, fmt.Sprintf("%q.%q", declaration.ID, scaffold.info.Name), func(foundation.Resolver) (manifest.Distribution, error) { return scaffold.info, nil })
}

func Resolve[T any](resolver foundation.Resolver, owner manifest.ID, id ID) (*Scaffold[T], error) {
	return foundation.Resolve(resolver, scaffoldKey[T](owner, id))
}

// Declarations lists scaffold metadata without invoking renderers or boot hooks.
func Declarations(resolver foundation.Resolver) ([]manifest.Distribution, error) {
	return foundation.Contributions(resolver, declarations)
}
