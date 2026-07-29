// Package migrate previews and applies canonical profile imports.
package migrate

import (
	"context"
	"errors"
	"fmt"

	"inferencerig/core/profiles"
)

// Candidate is one source profile translated into canonical YAML.
type Candidate struct {
	Name, SourcePath, ProfileYAML string
	Warnings                      []string
}

// Importer reads one legacy format without mutating it.
type Importer interface {
	Preview(context.Context) ([]Candidate, error)
}

// Item is one validated import candidate.
type Item struct {
	Candidate
	Profile profiles.ProfileDocument
}

// Plan is an immutable, previewable import batch.
type Plan struct {
	Items []Item
}

// Result reports create-only application of a plan.
type Result struct {
	Created []string
	Skipped []string
}

// Service validates and creates canonical profiles.
type Service struct {
	store *profiles.FileStore
}

// NewService creates a neutral migration service.
func NewService(store *profiles.FileStore) *Service {
	if store == nil {
		panic("migrate: profile store is required")
	}
	return &Service{store: store}
}

// Preview reads and validates candidates without writing destination profiles.
func (s *Service) Preview(ctx context.Context, importer Importer) (Plan, error) {
	if importer == nil {
		return Plan{}, fmt.Errorf("migrate: importer is required")
	}
	candidates, err := importer.Preview(ctx)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Items: make([]Item, 0, len(candidates))}
	for _, candidate := range candidates {
		profile, err := s.store.Validate(ctx, profiles.CreateRequest{
			Name: candidate.Name, ProfileYAML: candidate.ProfileYAML,
		})
		if err != nil {
			return Plan{}, fmt.Errorf("validate imported profile %q: %w", candidate.Name, err)
		}
		plan.Items = append(plan.Items, Item{Candidate: candidate, Profile: profile})
	}
	return plan, nil
}

// Apply creates plan profiles and skips existing destinations. It never replaces
// a destination profile and never reads or writes the source installation.
func (s *Service) Apply(ctx context.Context, plan Plan) (Result, error) {
	result := Result{}
	for _, item := range plan.Items {
		_, err := s.store.Create(ctx, profiles.CreateRequest{
			Name: item.Name, ProfileYAML: item.ProfileYAML,
		})
		if errors.Is(err, profiles.ErrExists) {
			result.Skipped = append(result.Skipped, item.Name)
			continue
		}
		if err != nil {
			return result, fmt.Errorf("create imported profile %q: %w", item.Name, err)
		}
		result.Created = append(result.Created, item.Name)
	}
	return result, nil
}
