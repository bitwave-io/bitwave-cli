package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/bitwave-io/bitwave-accounting-sdk/format"
	"github.com/bitwave-io/bitwave-accounting-sdk/model"
	legacystore "github.com/bitwave-io/bitwave-accounting-sdk/store"
	"github.com/bitwave-io/bitwave-cli/internal/bitwave/workspaces"
)

// Cloud is the bitwave-flavored cloud store. It composes the legacy
// internal/ledger/store.CloudStore (which already speaks the gl-svc HTTP
// protocol with per-call journal ids) with a workspaces.Client for journal
// CRUD that the legacy store doesn't expose.
type Cloud struct {
	cloud      *legacystore.CloudStore
	workspaces *workspaces.Client
	orgId      string
	wsId       string
}

// NewCloud builds a bitwave cloud store.
func NewCloud(baseURL, orgId, workspaceId string, tokenResolver func() (string, error)) *Cloud {
	return &Cloud{
		cloud:      legacystore.NewCloud(baseURL, orgId, workspaceId, tokenResolver),
		workspaces: workspaces.New(baseURL, orgId, tokenResolver),
		orgId:      orgId,
		wsId:       workspaceId,
	}
}

func (c *Cloud) Project(ctx context.Context) (*model.Project, error) {
	return c.cloud.Project(ctx)
}

func (c *Cloud) Journals(ctx context.Context) ([]string, error) {
	js, err := c.workspaces.ListJournals(c.wsId)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(js))
	for i, j := range js {
		out[i] = j.Id
	}
	return out, nil
}

func (c *Cloud) EnsureJournal(ctx context.Context, journalId string) error {
	if journalId == "" {
		return fmt.Errorf("journal id is required")
	}
	js, err := c.workspaces.ListJournals(c.wsId)
	if err != nil {
		return err
	}
	for _, j := range js {
		if j.Id == journalId {
			return nil
		}
	}
	_, err = c.workspaces.CreateJournal(c.wsId, workspaces.CreateJournalRequest{
		Id:   journalId,
		Name: titleFromId(journalId),
	})
	return err
}

func (c *Cloud) AddAccount(ctx context.Context, a model.Account) error {
	return c.cloud.AddAccount(ctx, a)
}

func (c *Cloud) AddPrice(ctx context.Context, p model.Price) error {
	return c.cloud.AddPrice(ctx, p)
}

func (c *Cloud) AddEntry(ctx context.Context, journalId string, e model.Entry) (string, error) {
	return c.cloud.AddEntryToJournal(ctx, journalId, e)
}

func (c *Cloud) SetEntryStatus(ctx context.Context, entryID string, status model.Status, postingAccount string) error {
	return c.cloud.SetEntryStatus(ctx, entryID, status, postingAccount)
}

func (c *Cloud) Import(ctx context.Context, journalId string, p *model.Project) error {
	var buf strings.Builder
	if err := format.Print(&buf, p); err != nil {
		return err
	}
	return c.cloud.ImportToJournal(ctx, journalId, buf.String())
}

// titleFromId upper-cases the first rune of every "-" or " " separated word.
func titleFromId(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
