package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// Topics group the conversations by subject. A new conversation is sorted into one just before it is analyzed, so the
// analysis can read the other conversations of the subject; the whole set is looked at again from time to time (merge
// what is one subject, rename what changed) and once for the conversations that existed before topics did.
const (
	// organizeEvery is how many conversations are sorted before the topics are looked at again as a whole, and
	// organizeGap the least time between two such looks.
	organizeEvery = 5
	organizeGap   = 30 * time.Minute
	// organizeRetry is how long to wait after a failed attempt before the next one.
	organizeRetry = 10 * time.Minute
	// organizeBatch is how many conversations without a topic one turn places; organizeRounds bounds the turns of one run.
	organizeBatch  = 25
	organizeRounds = 8
)

// TopicConfig is what the turns that sort conversations into topics need.
type TopicConfig struct {
	// SortSchemaFile and OrganizeSchemaFile hold the schemas their answers follow: one new conversation, the whole set.
	SortSchemaFile, OrganizeSchemaFile string
	// Dir is the folder these turns run in. They need no file, so it holds none: what a stranger wrote in a message
	// cannot make them read a project.
	Dir string
}

// topicsOn says whether conversations are sorted into topics.
func (s *Service) topicsOn() bool { return s.deps.Topics != nil }

// ask runs one read-only turn of the AI that belongs to no conversation, in an empty folder, and returns what it answered.
func (s *Service) ask(ctx context.Context, prompt, schemaFile string) (string, error) {
	if s.deps.Topics == nil {
		return "", i18n.NewError("invalid input")
	}
	selection := s.deps.Selection()
	provider, ok := s.deps.Providers[selection.Provider]
	if !ok {
		return "", i18n.NewError("%s is not available", selection.Provider.Name())
	}
	result, err := provider.Run(i18n.WithLanguage(ctx, s.deps.Language()), Turn{Prompt: prompt, Dir: s.deps.Topics.Dir, Sandbox: ReadOnly, Model: selection.Model, SchemaFile: schemaFile})
	return result.FinalMessage, err
}

// TopicChoice is where the AI sorted one conversation: an existing topic, or a new one when TopicID is empty.
type TopicChoice struct {
	TopicID string `json:"topicId"`
	Title   string `json:"title"`
}

// ParseTopicChoice validates the final message of a sorting turn.
func ParseTopicChoice(text string) (TopicChoice, error) {
	var choice TopicChoice
	if err := json.Unmarshal([]byte(text), &choice); err != nil {
		return TopicChoice{}, i18n.Wrap(ErrOutcome, "The AI did not answer with the expected JSON")
	}
	// The title is made a title by title(), after the secrets are taken out of it.
	choice.TopicID, choice.Title = strings.TrimSpace(choice.TopicID), strings.TrimSpace(choice.Title)
	return choice, nil
}

// sortIntoTopic puts a conversation that has no topic in one. It never stops the analysis that follows: a conversation the
// AI could not sort stays without a topic, and the next reorganization takes it.
func (s *Service) sortIntoTopic(ctx context.Context, id string) {
	if !s.topicsOn() {
		return
	}
	request, ok := s.deps.Store.Get(id)
	if !ok || request.TopicID != "" || s.pausedFor(s.deps.Selection().Provider) {
		return
	}
	topics := s.deps.Store.Topics()
	prompt := s.deps.Persona().TopicPrompt(request, topicList(topics, s.deps.Store.List()))
	answer, err := s.ask(ctx, prompt, s.deps.Topics.SortSchemaFile)
	if err == nil {
		var choice TopicChoice
		if choice, err = ParseTopicChoice(answer); err == nil {
			s.place(id, choice, topics)
			return
		}
	}
	// A stopped turn is the user's doing, not a problem.
	if !errors.Is(err, ErrCancelled) && ctx.Err() == nil {
		s.deps.OnError(fmt.Errorf("could not sort a conversation into a topic: %w", err))
	}
}

// title makes what the AI wrote a title: the secrets Pouple holds are taken out before the text is shortened, which could
// cut one in two and leave it unrecognizable.
func (s *Service) title(raw string) string { return cleanTitle(s.deps.Redact()(raw)) }

// place applies where the AI sorted a conversation: into the topic it named when that exists (possibly with a better
// title), otherwise into a new topic, titled by the AI or, failing that, by the start of the message.
func (s *Service) place(id string, choice TopicChoice, topics []Topic) {
	now := s.now()
	if i := slices.IndexFunc(topics, func(t Topic) bool { return t.ID == choice.TopicID }); i >= 0 {
		if title := s.title(choice.Title); s.deps.Store.PlaceInTopic(id, choice.TopicID, now) && title != "" {
			s.deps.Store.RetitleTopic(choice.TopicID, title, now)
		}
		return
	}
	title := s.title(choice.Title)
	if title == "" {
		if request, ok := s.deps.Store.Get(id); ok {
			title = s.title(request.Text)
		}
	}
	if title == "" {
		return
	}
	topic := Topic{ID: newID(), Title: title, CreatedAt: now, UpdatedAt: now}
	s.deps.Store.AddTopic(topic)
	if !s.deps.Store.PlaceInTopic(id, topic.ID, now) {
		// The conversation was removed while the AI answered: its topic would be empty.
		s.deps.Store.DropEmptyTopics()
	}
}

// sharedReader says whether what the AI writes for a request may reach someone else than the user: then it reads nothing
// private (another channel's conversations of its topic, a private fact of the memory). Only a note to oneself, a finished pipeline and a reminder have no one to answer; an Asana comment and a
// review of a merge request get a reply drafted for their author.
func sharedReader(r Stored) bool {
	switch r.Source.Origin {
	case "note-to-self", OriginPipeline, OriginReminder:
		return false
	}
	return true
}

// relatedConversationsOf are the other conversations of the topic of a request that carry something to read, oldest
// first, and whether any of them holds text written by other people.
//
// What the AI writes in a conversation that came from someone else may be sent to that person: it then reads only the
// conversations of its own channel, as it did before topics, since the others may be private (a direct message, a note to
// oneself). An Asana comment and a merge request review are answered too: they read only the conversations of their own
// service. A note to oneself, a reminder and a finished pipeline have no such reader and read the whole topic.
func (s *Service) relatedConversationsOf(request Stored) (related []Stored, foreign bool) {
	if !s.topicsOn() || request.TopicID == "" {
		return nil, false
	}
	shared := sharedReader(request)
	for _, other := range s.deps.Store.List() {
		if other.ID == request.ID || other.TopicID != request.TopicID || other.Status == StatusIgnored || other.Status.Pending() {
			continue
		}
		if shared && other.Source.ChannelID != request.Source.ChannelID {
			continue
		}
		related = append(related, other)
	}
	if len(related) > relatedConversations {
		related = related[len(related)-relatedConversations:]
	}
	for _, other := range related {
		if !s.ownText(other, 0) {
			foreign = true
		}
	}
	return related, foreign
}

// maxOwnDepth bounds how far a reminder is followed back to the conversation it came from.
const maxOwnDepth = 3

// ownText says whether a conversation holds only what the user wrote or approved: a note to themselves that no service or
// web page shaped, or a reminder set from such a note. Anything else carries text written by other people.
func (s *Service) ownText(r Stored, depth int) bool {
	if r.ServicesUsed || r.WebUsed || r.ForeignSinceGrant {
		return false
	}
	switch r.Source.Origin {
	case "note-to-self":
		return true
	case OriginReminder:
		// A reminder's title and note were written by the AI from the conversation it was suggested in.
		origin, ok := s.deps.Store.Get(r.Source.RelatedRequestID)
		return ok && depth < maxOwnDepth && s.ownText(origin, depth+1)
	}
	return false
}

// OrganizeAnswer is what the AI decided when it looked at all the topics.
type OrganizeAnswer struct {
	Places []struct {
		RequestID string `json:"requestId"`
		TopicID   string `json:"topicId"`
		Title     string `json:"title"`
	} `json:"places"`
	Merges []struct {
		From string `json:"from"`
		Into string `json:"into"`
	} `json:"merges"`
	Renames []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"renames"`
}

// ParseOrganizeAnswer validates the final message of an organizing turn.
func ParseOrganizeAnswer(text string) (OrganizeAnswer, error) {
	var answer OrganizeAnswer
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		return OrganizeAnswer{}, i18n.Wrap(ErrOutcome, "The AI did not answer with the expected JSON")
	}
	return answer, nil
}

// sortable says whether a conversation waits for a topic: it is not being worked on (its own turn sorts it first) and
// said something (a message that needed nothing stays out of the topics).
func sortable(r Stored) bool {
	return r.TopicID == "" && !r.Status.Pending() && r.Status != StatusIgnored
}

// withoutTopic are the conversations waiting for a topic, oldest first.
func (s *Service) withoutTopic() []Stored {
	var waiting []Stored
	for _, request := range s.deps.Store.List() {
		if sortable(request) {
			waiting = append(waiting, request)
		}
	}
	return waiting
}

// maybeOrganize schedules a look at all the topics when one is due: conversations wait for a topic (those that existed
// before topics did, or that could not be sorted), or enough were sorted since the last look.
func (s *Service) maybeOrganize(ctx context.Context) {
	if !s.topicsOn() || s.pausedFor(s.deps.Selection().Provider) {
		return
	}
	now := s.deps.Now()
	organization := s.deps.Store.Organization()
	last, _ := time.Parse(time.RFC3339, organization.At)
	due := organization.Placed >= organizeEvery && now.Sub(last) >= organizeGap
	if !due && len(s.withoutTopic()) == 0 {
		return
	}
	s.mu.Lock()
	if s.organizing || s.closing || now.Sub(s.organizeTried) < organizeRetry {
		s.mu.Unlock()
		return
	}
	s.organizing, s.organizeTried = true, now
	s.mu.Unlock()
	s.queue.Add(Background, func() {
		defer func() {
			s.mu.Lock()
			s.organizing = false
			s.mu.Unlock()
		}()
		if err := s.organize(ctx); err != nil && !errors.Is(err, ErrCancelled) && ctx.Err() == nil {
			s.deps.OnError(fmt.Errorf("could not organize the topics: %w", err))
		}
	})
}

// organize looks at all the topics: it places the conversations that have none (a batch at a time), merges the topics
// that are one subject and renames those whose title changed.
func (s *Service) organize(ctx context.Context) error {
	for round := 0; round < organizeRounds; round++ {
		all := s.withoutTopic()
		waiting := all
		if len(waiting) > organizeBatch {
			waiting = waiting[:organizeBatch]
		}
		topics := s.deps.Store.Topics()
		if len(topics) == 0 && len(waiting) == 0 {
			break
		}
		prompt := s.deps.Persona().OrganizePrompt(topicList(topics, s.deps.Store.List()), unsortedList(waiting))
		text, err := s.ask(ctx, prompt, s.deps.Topics.OrganizeSchemaFile)
		if err != nil {
			return err
		}
		answer, err := ParseOrganizeAnswer(text)
		if err != nil {
			return err
		}
		s.applyOrganization(answer, waiting)
		// Another round only while the last one placed something: what the AI left aside would be asked again for nothing.
		if left := len(s.withoutTopic()); left == 0 || left >= len(all) {
			break
		}
	}
	s.deps.Store.DropEmptyTopics()
	s.deps.Store.Organized(s.now())
	return nil
}

// applyOrganization carries out what the AI decided, keeping only what refers to things that exist: an id it made up
// changes nothing.
func (s *Service) applyOrganization(answer OrganizeAnswer, waiting []Stored) {
	now := s.now()
	asked := map[string]bool{}
	for _, request := range waiting {
		asked[request.ID] = true
	}
	// A new key of the AI's own names a topic that is made when its first conversation is placed.
	made := map[string]string{}
	for _, place := range answer.Places {
		if !asked[place.RequestID] {
			continue
		}
		key := strings.TrimSpace(place.TopicID)
		topicID := key
		if !slices.ContainsFunc(s.deps.Store.Topics(), func(t Topic) bool { return t.ID == key }) {
			if made[key] == "" {
				title := s.title(place.Title)
				if title == "" {
					continue
				}
				topic := Topic{ID: newID(), Title: title, CreatedAt: now, UpdatedAt: now}
				s.deps.Store.AddTopic(topic)
				made[key] = topic.ID
			}
			topicID = made[key]
		}
		if s.deps.Store.PlaceInTopic(place.RequestID, topicID, now) {
			delete(asked, place.RequestID)
		}
	}
	for _, merge := range answer.Merges {
		s.deps.Store.MergeTopics(strings.TrimSpace(merge.From), strings.TrimSpace(merge.Into), now)
	}
	for _, rename := range answer.Renames {
		if title := s.title(rename.Title); title != "" {
			s.deps.Store.RetitleTopic(strings.TrimSpace(rename.ID), title, now)
		}
	}
}
