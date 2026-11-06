package connector

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Localized returns a display copy of the descriptor.
func (d Descriptor) Localized(lang i18n.Lang) Descriptor {
	d.Tagline = i18n.T(lang, d.Tagline)
	d.Fields = slices.Clone(d.Fields)
	for i, field := range d.Fields {
		field.Label = i18n.T(lang, field.Label)
		field.Help = i18n.T(lang, field.Help)
		field.PatternHint = i18n.T(lang, field.PatternHint)
		d.Fields[i] = field
	}
	d.Guide = slices.Clone(d.Guide)
	for i, step := range d.Guide {
		step.Title = i18n.T(lang, step.Title)
		step.Body = i18n.T(lang, step.Body)
		if step.Link != nil {
			link := *step.Link
			link.Label = i18n.T(lang, link.Label)
			step.Link = &link
		}
		d.Guide[i] = step
	}
	d.Actions = slices.Clone(d.Actions)
	for i, action := range d.Actions {
		action.Label = i18n.T(lang, action.Label)
		d.Actions[i] = action
	}
	return d
}

// Localized translates app-authored profile fields at read time, including profiles already on disk.
func (p Profile) Localized(lang i18n.Lang, connectorID string) Profile {
	if lang == i18n.EN {
		return p
	}
	if connectorID == "codex" && p.Name == "OpenAI API key" {
		p.Name = i18n.T(lang, p.Name)
	}
	if p.Subtitle == "Billed per use" {
		p.Subtitle = i18n.T(lang, p.Subtitle)
	}
	p.Facts = slices.Clone(p.Facts)
	var until time.Time
	for i, fact := range p.Facts {
		switch fact.Label {
		case "Permissions":
			var granted, total int
			if _, err := fmt.Sscanf(fact.Value, "%d of %d granted", &granted, &total); err == nil {
				fact.Value = i18n.T(lang, "%d of %d granted", granted, total)
			}
		case "Token expires":
			if date, err := time.Parse("2 Jan 2006", fact.Value); err == nil {
				fact.Value = i18n.Date(lang, date)
			}
		case "Subscription until":
			if date, err := time.Parse("2 Jan 2006", fact.Value); err == nil {
				until = date
				fact.Value = i18n.Date(lang, date)
			}
		}
		fact.Label = i18n.T(lang, fact.Label)
		p.Facts[i] = fact
	}
	p.Warnings = slices.Clone(p.Warnings)
	for i, warning := range p.Warnings {
		title := warning.Title
		if scope, ok := strings.CutPrefix(title, "Missing "); ok {
			warning.Title = i18n.T(lang, "Missing %s", scope)
		} else {
			warning.Title = i18n.T(lang, title)
		}
		if title == "Subscription ended" && !until.IsZero() {
			warning.Detail = i18n.T(lang, "This ChatGPT plan ended on %s: Codex may refuse requests.", i18n.Date(lang, until))
		} else {
			warning.Detail = i18n.T(lang, warning.Detail)
		}
		p.Warnings[i] = warning
	}
	return p
}
