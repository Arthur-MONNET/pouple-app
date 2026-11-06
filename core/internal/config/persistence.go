package config

import "github.com/Arthur-MONNET/pouple/core/internal/connector"

// Connectors adapts the settings file to what the connector registry stores.
type Connectors struct{ File *File }

func (c Connectors) Load(id string) (connector.Saved, bool, error) {
	settings, err := c.File.Load()
	if err != nil {
		return connector.Saved{}, false, err
	}
	state, ok := settings.Connectors[id]
	return connector.Saved{Values: state.Values, Profile: state.Profile, ConnectedAt: state.ConnectedAt}, ok, nil
}

func (c Connectors) Save(id string, saved connector.Saved) error {
	_, err := c.File.Update(func(s *Settings) {
		s.Connectors[id] = ConnectorState{Values: saved.Values, Profile: saved.Profile, ConnectedAt: saved.ConnectedAt}
	})
	return err
}

func (c Connectors) Remove(id string) error {
	_, err := c.File.Update(func(s *Settings) { delete(s.Connectors, id) })
	return err
}
