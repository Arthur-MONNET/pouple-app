package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/asana"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// The approved payload identifies a replay even if the assistant changes the plan's wording
// or proposes it in another conversation. An empty receipt means the POST may have happened.
func asanaTaskKey(action Action) string {
	payload, _ := json.Marshal([3]string{action.Scope.ID, action.Title, action.Description})
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func (s *Service) createAsanaTask(ctx context.Context, id string, action Action) bool {
	report := func(role, line string) {
		s.update(id, func(r *Stored) { r.Entries = append(r.Entries, s.entry(role, "text", line)) })
	}
	if ctx.Err() != nil {
		report("system", s.text("Asana: “%s” was skipped after task creation stopped.", action.Title))
		return false
	}
	if s.deps.CreateAsanaTask == nil {
		report("system", s.text("Asana task creation is unavailable."))
		return false
	}

	key := asanaTaskKey(action)
	link, reserved, err := s.deps.Store.reserveAsanaTask(key)
	if err != nil {
		report("system", s.text("Asana: could not save the creation attempt for “%s”.", action.Title))
		return false
	}
	if reserved {
		link, err = s.deps.CreateAsanaTask(ctx, action.Scope.ID, action.Title, action.Description)
		if err == nil && link != "" {
			s.deps.Store.finishAsanaTask(key, link)
		} else if err != nil && !errors.Is(err, asana.ErrCreationUnconfirmed) {
			s.deps.Store.finishAsanaTask(key, "")
			report("system", s.text("Asana: could not create “%s” (%s)", action.Title, i18n.ErrorText(s.deps.Language(), err)))
			return false
		}
	}
	if link == "" {
		report("system", s.text("Asana: creation of “%s” is unconfirmed. Check the project in Asana.", action.Title))
		return false
	}
	title := strings.NewReplacer("[", "［", "]", "］").Replace(action.Title)
	link = strings.NewReplacer("(", "%28", ")", "%29").Replace(link)
	message := "Asana: created [%s](%s)"
	if !reserved {
		message = "Asana: already created [%s](%s)"
	}
	report("assistant", s.text(message, title, link))
	return true
}
