package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// ProviderTest is the outcome of a minimal turn run on one provider.
type ProviderTest struct {
	// Model is the model the CLI reported, else the one asked for ("" is the CLI's default).
	Model   string
	Latency time.Duration
	// Reply is what the AI answered, shortened.
	Reply string
}

// providerTestTimeout bounds a test: it is a single word, so a longer wait means something is wrong.
var providerTestTimeout = 90 * time.Second

// ErrTestTimeout is returned (wrapped, localized) when a test takes longer than providerTestTimeout.
var ErrTestTimeout = errors.New("the test took too long")

const providerTestPrompt = "Reply with the single word ok. Do not use any tool."

// TestProvider runs one minimal read-only turn on the provider with the model, to show that the sign-in, the CLI and
// the model work together. It goes through the same metered provider as any turn, so it counts as one turn of the
// daily quota. The errors it returns are localized, and still match ErrLimit, ErrLogin and *QuotaError.
func (s *Service) TestProvider(ctx context.Context, id providers.ID, model string) (ProviderTest, error) {
	name := id.Name()
	provider, ok := s.deps.Providers[id]
	if !ok {
		return ProviderTest{}, i18n.NewError("%s is not available", name)
	}
	if !providers.ValidModel(model) {
		return ProviderTest{}, i18n.NewError("The model name “%s” is not valid", model)
	}
	testCtx, cancel := context.WithTimeout(i18n.WithLanguage(ctx, s.deps.Language()), providerTestTimeout)
	defer cancel()

	started := time.Now()
	result, err := provider.Run(testCtx, Turn{Prompt: providerTestPrompt, Dir: s.workDir(), Sandbox: ReadOnly, Model: model})
	latency := time.Since(started)
	switch {
	case err == nil:
	case ctx.Err() == nil && testCtx.Err() != nil:
		return ProviderTest{}, i18n.Wrap(ErrTestTimeout, "The test of %s took more than %d seconds and was stopped", name, int(providerTestTimeout.Seconds()))
	case errors.Is(err, ErrLimit):
		return ProviderTest{}, i18n.Wrap(err, "%s's usage limit is reached. Try again later.", name)
	case errors.Is(err, ErrLogin):
		return ProviderTest{}, i18n.Wrap(err, "%s is not signed in any more. Open Connectors to sign in again.", name)
	default:
		return ProviderTest{}, err
	}
	reply := strings.TrimSpace(result.FinalMessage)
	if reply == "" {
		return ProviderTest{}, i18n.NewError("%s finished without an answer", name)
	}
	// A provider that answers is a provider that works: the pause and the sign-in notice it may carry are over.
	s.mu.Lock()
	delete(s.pausedUntil, id)
	delete(s.loginProblem, id)
	s.mu.Unlock()
	s.broadcast()
	return ProviderTest{
		Model:   firstNonEmpty(result.Model, model),
		Latency: latency,
		Reply:   shortLine(s.deps.Redact()(reply)),
	}, nil
}
