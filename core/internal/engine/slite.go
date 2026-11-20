package engine

import (
	"context"
	"errors"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slite"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

func (s *Service) publishSlitePage(ctx context.Context, id string, action Action) bool {
	var line, role string
	role = "system"
	created := false
	switch {
	case ctx.Err() != nil:
		line = s.text("Slite: “%s” was skipped after publication stopped.", action.Title)
	case s.deps.CreateSlitePage == nil:
		line = s.text("Slite page creation is unavailable.")
	default:
		link, err := s.deps.CreateSlitePage(ctx, action.Scope.ID, action.Title, action.Markdown)
		switch {
		case errors.Is(err, slite.ErrCreationUnconfirmed):
			line = s.text("Slite: creation of “%s” is unconfirmed. Check Slite before requesting it again.", action.Title)
		case err != nil:
			line = s.text("Slite: could not create “%s” (%s)", action.Title, i18n.ErrorText(s.deps.Language(), err))
		default:
			title := strings.NewReplacer("[", "［", "]", "］").Replace(action.Title)
			link = strings.NewReplacer("(", "%28", ")", "%29").Replace(link)
			line, role, created = s.text("Slite: created [%s](%s)", title, link), "assistant", true
		}
	}
	s.recordSliteResult(id, role, line)
	return created
}

func (s *Service) recordSliteResult(id, role, line string) {
	s.update(id, func(r *Stored) {
		r.Entries = append(r.Entries, s.entry(role, "text", line))
	})
}
