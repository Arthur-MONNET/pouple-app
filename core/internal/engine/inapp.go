package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// ProjectInfo is a project folder the user can choose when they start a conversation in the app.
type ProjectInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// maxLabelRunes bounds the label of a conversation started in the app: the start of its first line.
const maxLabelRunes = 80

// StartConversation starts a conversation the user typed in the app, with the files they added (uploaded beforehand)
// and, optionally, the project folder they chose. It is the only way to create a request of origin OriginInApp: the
// user's own text, with the rights of a note to themselves. It is analysed at once, before what others sent, and is never
// skipped silently: when it cannot start, the error says why.
func (s *Service) StartConversation(text string, attachmentIDs []string, projectID string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" && len(attachmentIDs) == 0 {
		return "", i18n.Wrap(ErrInput, "write something or add a file")
	}
	if len(text) > maxMessageChars {
		return "", i18n.Wrap(ErrInput, "the message is longer than %d characters", maxMessageChars)
	}
	if projectID != "" && s.projectPath(projectID) == "" {
		return "", i18n.Wrap(ErrInput, "%s is not a project of your projects folder", projectID)
	}
	refs := make([]AttachmentRef, 0, len(attachmentIDs))
	for _, attachmentID := range attachmentIDs {
		ref, _, err := s.deps.Attachments.Open(attachmentID)
		if err != nil {
			return "", i18n.Wrap(ErrInput, "unknown attachment")
		}
		refs = append(refs, ref)
	}
	if err := s.requireActive(); err != nil {
		return "", err
	}
	waiting := 0
	for _, other := range s.deps.Store.List() {
		if other.Status == StatusQueued {
			waiting++
		}
	}
	if waiting >= maxQueued {
		return "", i18n.Wrap(ErrState, "too many requests are waiting: try again when some are done")
	}
	id := newID()
	received := s.now()
	label := conversationLabel(text, refs)
	request := Stored{Request: Request{
		ID: id,
		Source: Source{
			Origin: OriginInApp, ChannelID: "in-app:" + id, SenderID: "self", SenderName: s.deps.Persona().Name,
			TS: fmt.Sprintf("%d.000000", s.deps.Now().Unix()), Label: &label,
		},
		Text: text, Attachments: refs, ProjectID: projectID, ReceivedAt: received, UpdatedAt: received,
		Status: StatusQueued, Questions: []Question{}, Entries: []Entry{},
		// The user is looking at it: its bubble and its progress show from the start.
		Engaged: true,
	}}
	s.deps.Store.Add(request)
	s.enqueue(id, Interactive, func(ctx context.Context) error { return s.analyze(ctx, id) })
	return id, nil
}

// conversationLabel names a conversation of the app in a list: the start of its first line, or the name of its first
// file when it has no text.
func conversationLabel(text string, refs []AttachmentRef) string {
	first, _, _ := strings.Cut(text, "\n")
	first = strings.TrimSpace(first)
	if first == "" && len(refs) > 0 {
		first = refs[0].Name
	}
	if runes := []rune(first); len(runes) > maxLabelRunes {
		first = string(runes[:maxLabelRunes])
	}
	return first
}

// Projects lists the project folders the user can choose for a conversation, by name: the folders directly under the
// projects folder that are projects (no hidden folder, no link that leads out, nothing of Pouple's own). None when no
// projects folder is chosen.
func (s *Service) Projects() []ProjectInfo {
	projects := []ProjectInfo{}
	if s.deps.ProjectsRoot == nil {
		return projects
	}
	root := s.deps.ProjectsRoot()
	if root == "" {
		return projects
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return projects
	}
	// ReadDir returns the entries sorted by name.
	for _, entry := range entries {
		if name := entry.Name(); s.projectPath(name) != "" {
			projects = append(projects, ProjectInfo{ID: name, Name: name})
		}
	}
	return projects
}
