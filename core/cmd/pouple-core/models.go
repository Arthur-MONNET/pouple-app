package main

import (
	"context"

	"github.com/Arthur-MONNET/pouple/core/internal/connector"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/codex"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// modelCatalog says which models each AI offers: Claude's are fixed, Codex's come from its own catalog.
type modelCatalog struct {
	registry *connector.Registry
}

func (m modelCatalog) Models(ctx context.Context, id providers.ID) ([]providers.Suggestion, error) {
	if id != providers.Codex {
		return providers.Suggestions(id), nil
	}
	registered, err := m.registry.Get(codex.ID)
	if err != nil {
		return nil, err
	}
	source, ok := registered.(*codex.Connector)
	if !ok {
		return nil, connector.ErrUnknown
	}
	return source.Models(ctx)
}
