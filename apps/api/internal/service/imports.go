package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

// Job kinds processed by the background worker.
const (
	JobImportParse       = "import.parse"
	JobImportCommit      = "import.commit"
	JobEmbedEntity       = "embed.entity"
	JobEmbedConversation = "embed.conversation"
	JobEmbedBackfill     = "embed.backfill"
)

// ImportJob is the payload of import jobs.
type ImportJob struct {
	UserID   uuid.UUID `json:"user_id"`
	ImportID uuid.UUID `json:"import_id"`
}

// CreateImportInput starts an import from a file, pasted text or a public URL.
type CreateImportInput struct {
	SourceKind   string     `json:"source_kind"` // file | paste | url
	Filename     string     `json:"filename,omitempty"`
	ContentType  string     `json:"content_type,omitempty"`
	Data         []byte     `json:"-"`
	Text         string     `json:"text,omitempty"`
	URL          string     `json:"url,omitempty"`
	Adapter      string     `json:"adapter,omitempty"` // optional explicit adapter, e.g. "chatgpt/v1"
	TargetIdeaID *uuid.UUID `json:"target_idea_id,omitempty"`
}

func (in *CreateImportInput) normalize(maxBytes int64) error {
	in.SourceKind = strings.ToLower(strings.TrimSpace(in.SourceKind))
	if in.SourceKind == "" {
		switch {
		case in.URL != "":
			in.SourceKind = "url"
		case len(in.Data) > 0:
			in.SourceKind = "file"
		default:
			in.SourceKind = "paste"
		}
	}
	switch in.SourceKind {
	case "url":
		u := strings.TrimSpace(in.URL)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return domain.Invalid("url", "URL must start with http:// or https://")
		}
		in.URL = u
	case "paste":
		if strings.TrimSpace(in.Text) == "" && len(in.Data) == 0 {
			return domain.Invalid("text", "paste some conversation text")
		}
		if len(in.Data) == 0 {
			in.Data = []byte(in.Text)
		}
		if in.Filename == "" {
			in.Filename = "pasted.md"
		}
	case "file":
		if len(in.Data) == 0 {
			return domain.Invalid("file", "the uploaded file is empty")
		}
	default:
		return domain.Invalid("source_kind", "source_kind must be file, paste or url")
	}
	if int64(len(in.Data)) > maxBytes {
		return domain.Invalid("file", fmt.Sprintf("file exceeds the %d MB limit", maxBytes>>20))
	}
	// Never trust client filenames: keep only a safe base name.
	in.Filename = safeFilename(in.Filename)
	return nil
}

func safeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if r == '.' || r == '-' || r == '_' || r == ' ' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			b.WriteRune(r)
		}
	}
	out := strings.TrimLeft(strings.TrimSpace(b.String()), ".")
	if len(out) > 120 {
		out = out[len(out)-120:]
	}
	return out
}

func (s *Service) maxUpload() int64 {
	if s.cfg != nil && s.cfg.MaxUploadBytes > 0 {
		return s.cfg.MaxUploadBytes
	}
	return 200 << 20
}

// CreateImport stores the raw input and queues asynchronous parsing.
func (s *Service) CreateImport(ctx context.Context, a Actor, in CreateImportInput) (*domain.Import, error) {
	if err := in.normalize(s.maxUpload()); err != nil {
		return nil, err
	}
	if in.TargetIdeaID != nil {
		if _, err := s.store.GetIdea(ctx, a.UserID, *in.TargetIdeaID); err != nil {
			return nil, err
		}
	}
	sum := sha256.Sum256(append(in.Data, []byte(in.URL)...))
	im := &domain.Import{SourceKind: in.SourceKind, Adapter: in.Adapter, Filename: in.Filename, URI: in.URL, ByteSize: int64(len(in.Data)),
		ContentHash: hex.EncodeToString(sum[:]), Options: map[string]any{}, TargetIdeaID: in.TargetIdeaID}
	if err := s.store.CreateImport(ctx, a.UserID, im, in.Data); err != nil {
		return nil, err
	}
	uid := a.UserID
	if _, err := s.store.EnqueueJob(ctx, &uid, JobImportParse, ImportJob{UserID: a.UserID, ImportID: im.ID}, "import-parse:"+im.ID.String(), time.Time{}); err != nil {
		return nil, err
	}
	return im, nil
}

// parseImport runs the adapters over an import's raw input.
func (s *Service) parseImport(ctx context.Context, userID uuid.UUID, im *domain.Import) (*imports.ParseResult, error) {
	if s.parser == nil {
		return nil, domain.Unavailable("import adapters are not configured")
	}
	if im.SourceKind == "url" {
		return s.parser.ParseURL(ctx, s.fetcher, im.URI)
	}
	raw, err := s.store.ImportRaw(ctx, userID, im.ID)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("import payload already processed or missing")
	}
	return s.parser.Parse(ctx, imports.Input{Filename: im.Filename, Data: raw}, im.Adapter)
}

// ProcessImportParse is the job handler: parse → dedupe → injection scan → PREVIEW.
func (s *Service) ProcessImportParse(ctx context.Context, userID, importID uuid.UUID) error {
	im, err := s.store.GetImport(ctx, userID, importID)
	if err != nil {
		return err
	}
	if im.Status != domain.ImportQueued && im.Status != domain.ImportProcessing {
		return nil
	}
	st := domain.ImportProcessing
	_ = s.store.UpdateImport(ctx, im.ID, postgres.ImportUpdate{Status: &st})
	res, err := s.parseImport(ctx, userID, im)
	if err != nil {
		fail := domain.ImportFailed
		msg := userFacingImportError(err)
		_ = s.store.UpdateImport(ctx, im.ID, postgres.ImportUpdate{Status: &fail, Error: &msg, ClearRaw: true, Completed: true})
		return nil // terminal, not retried
	}
	items, failed, err := s.stageItems(ctx, userID, im.ID, res)
	if err != nil {
		return err
	}
	status := domain.ImportPreview
	if items == 0 {
		status = domain.ImportFailed
	}
	errMsg := ""
	if items == 0 {
		errMsg = "No conversations could be read from this input."
	}
	total := items + failed
	warnings := res.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return s.store.UpdateImport(ctx, im.ID, postgres.ImportUpdate{Status: &status, Provider: &res.Provider, Adapter: &res.Adapter, TotalItems: &total,
		FailedItems: &failed, Warnings: warnings, Error: &errMsg, ClearRaw: true, Completed: status == domain.ImportFailed})
}

func userFacingImportError(err error) string {
	switch {
	case errors.Is(err, imports.ErrPrivateURL):
		return err.Error()
	case errors.Is(err, imports.ErrUnsupportedFormat):
		return "Unsupported format. Supported: ChatGPT export (conversations.json or .zip), Claude export, Gemini Takeout (MyActivity.json), Markdown, JSON, plain text, and public share links. " + err.Error()
	case errors.Is(err, imports.ErrTooLarge):
		return "The input is too large."
	case errors.Is(err, imports.ErrEmptyInput):
		return "The input is empty."
	}
	return "Import failed: " + err.Error()
}

// stageItems writes parsed conversations as import items with dedupe status and injection flags.
func (s *Service) stageItems(ctx context.Context, userID, importID uuid.UUID, res *imports.ParseResult) (int, int, error) {
	n := 0
	for i, conv := range res.Conversations {
		payload, err := json.Marshal(conv)
		if err != nil {
			return 0, 0, err
		}
		hash := imports.ContentHash(conv)
		it := &domain.ImportItem{ImportID: importID, Position: i, ExternalID: conv.ExternalID, Title: trimTo(conv.Title, 300), Status: "PENDING",
			MessageCount: len(conv.Messages), StartedAt: conv.CreatedAt, ContentHash: hash, Payload: payload}
		flags := map[string]bool{}
		for _, m := range conv.Messages {
			for _, f := range imports.ScanInjection(m.Content) {
				flags[f] = true
			}
		}
		for f := range flags {
			it.InjectionFlags = append(it.InjectionFlags, f)
		}
		if dup := s.findDuplicateConversation(ctx, userID, conv, hash); dup != nil {
			it.Status = "DUPLICATE"
			it.DuplicateOf = &dup.ID
		}
		if err := s.store.InsertImportItem(ctx, userID, it); err != nil {
			return 0, 0, err
		}
		n++
	}
	for j, f := range res.Failures {
		it := &domain.ImportItem{ImportID: importID, Position: len(res.Conversations) + j, ExternalID: f.ExternalID, Title: trimTo(f.Title, 300), Status: "FAILED",
			Payload: json.RawMessage(`{}`), Error: f.Error}
		if err := s.store.InsertImportItem(ctx, userID, it); err != nil {
			return 0, 0, err
		}
	}
	return n, len(res.Failures), nil
}

func (s *Service) findDuplicateConversation(ctx context.Context, userID uuid.UUID, conv imports.NormalizedConversation, hash string) *domain.Conversation {
	if conv.ExternalID != "" {
		if c, err := s.store.FindConversationByExternal(ctx, userID, conv.Provider, conv.ExternalID); err == nil {
			return c
		}
	}
	if c, err := s.store.FindConversationBySourceHash(ctx, userID, hash); err == nil {
		return c
	}
	return nil
}

// ImportDetail is an import with its items.
type ImportDetail struct {
	Import *domain.Import      `json:"import"`
	Items  []domain.ImportItem `json:"items"`
}

// GetImport returns an import and its items (payloads omitted).
func (s *Service) GetImport(ctx context.Context, userID, id uuid.UUID) (*ImportDetail, error) {
	im, err := s.store.GetImport(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	items, err := s.store.ListImportItems(ctx, userID, id, false, nil)
	if err != nil {
		return nil, err
	}
	return &ImportDetail{Import: im, Items: items}, nil
}

// CommitItem selects one parsed conversation and decides where it goes.
type CommitItem struct {
	ItemID       uuid.UUID  `json:"item_id"`
	Include      bool       `json:"include"`
	IdeaID       *uuid.UUID `json:"idea_id,omitempty"`
	NewIdea      bool       `json:"new_idea,omitempty"`
	NewIdeaTitle string     `json:"new_idea_title,omitempty"`
}

// CommitImportInput confirms what to import after previewing.
type CommitImportInput struct {
	Items   []CommitItem `json:"items"`
	Extract bool         `json:"extract"`
	// IncludeDuplicates re-imports conversations already in the vault (default false).
	IncludeDuplicates bool `json:"include_duplicates,omitempty"`
}

type commitOptions struct {
	Extract bool                     `json:"extract"`
	Items   map[string]commitItemOpt `json:"items"`
}

type commitItemOpt struct {
	IdeaID       *uuid.UUID `json:"idea_id,omitempty"`
	NewIdea      bool       `json:"new_idea,omitempty"`
	NewIdeaTitle string     `json:"new_idea_title,omitempty"`
}

// CommitImport records the user's selection and queues the commit job.
func (s *Service) CommitImport(ctx context.Context, a Actor, importID uuid.UUID, in CommitImportInput) (*domain.Import, error) {
	im, err := s.store.GetImport(ctx, a.UserID, importID)
	if err != nil {
		return nil, err
	}
	if im.Status != domain.ImportPreview {
		return nil, domain.Conflict(fmt.Sprintf("import is %s; only PREVIEW imports can be committed", im.Status))
	}
	items, err := s.store.ListImportItems(ctx, a.UserID, importID, false, nil)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]domain.ImportItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	opts := commitOptions{Extract: in.Extract, Items: map[string]commitItemOpt{}}
	selected := 0
	for _, ci := range in.Items {
		it, ok := byID[ci.ItemID]
		if !ok {
			return nil, domain.Invalid("items", "unknown import item")
		}
		if it.Status == "FAILED" {
			continue
		}
		if ci.IdeaID != nil {
			if _, err := s.store.GetIdea(ctx, a.UserID, *ci.IdeaID); err != nil {
				return nil, err
			}
		}
		status := "SKIPPED"
		if ci.Include && (it.Status != "DUPLICATE" || in.IncludeDuplicates) {
			status = "SELECTED"
			selected++
			opts.Items[ci.ItemID.String()] = commitItemOpt{IdeaID: ci.IdeaID, NewIdea: ci.NewIdea, NewIdeaTitle: ci.NewIdeaTitle}
		}
		if err := s.store.UpdateImportItem(ctx, ci.ItemID, postgres.ImportItemUpdate{Status: &status}); err != nil {
			return nil, err
		}
	}
	if selected == 0 {
		return nil, domain.Invalid("items", "select at least one conversation to import")
	}
	b, _ := json.Marshal(opts)
	var optMap map[string]any
	_ = json.Unmarshal(b, &optMap)
	st := domain.ImportProcessing
	stage := "commit"
	zero := 0
	if err := s.store.UpdateImport(ctx, importID, postgres.ImportUpdate{Status: &st, Stage: &stage, ProcessedItems: &zero, TotalItems: &selected}); err != nil {
		return nil, err
	}
	if err := s.store.UpdateImportOptions(ctx, importID, optMap); err != nil {
		return nil, err
	}
	uid := a.UserID
	if _, err := s.store.EnqueueJob(ctx, &uid, JobImportCommit, ImportJob{UserID: a.UserID, ImportID: importID}, "import-commit:"+importID.String(), time.Time{}); err != nil {
		return nil, err
	}
	return s.store.GetImport(ctx, a.UserID, importID)
}

// ProcessImportCommit is the job handler that persists selected items.
func (s *Service) ProcessImportCommit(ctx context.Context, userID, importID uuid.UUID) error {
	im, err := s.store.GetImport(ctx, userID, importID)
	if err != nil {
		return err
	}
	if im.Status != domain.ImportProcessing || im.Stage != "commit" {
		return nil
	}
	var opts commitOptions
	b, _ := json.Marshal(im.Options)
	_ = json.Unmarshal(b, &opts)
	items, err := s.store.ListImportItems(ctx, userID, importID, true, []string{"SELECTED"})
	if err != nil {
		return err
	}
	a := Actor{UserID: userID, Kind: domain.ActorImport}
	ok, failed := 0, 0
	for i := range items {
		it := items[i]
		o := opts.Items[it.ID.String()]
		if o.IdeaID == nil && !o.NewIdea && im.TargetIdeaID != nil {
			o.IdeaID = im.TargetIdeaID
		}
		_, _, _, err := s.commitItem(ctx, a, im, &it, o, opts.Extract)
		if err != nil {
			failed++
			msg := trimTo(err.Error(), 400)
			fs := "FAILED"
			_ = s.store.UpdateImportItem(ctx, it.ID, postgres.ImportItemUpdate{Status: &fs, Error: &msg})
			_ = s.store.IncrementImportProgress(ctx, importID, 0, 1)
			s.log.WarnContext(ctx, "import item failed", "import_id", importID, "item", it.ID, "error", err)
			continue
		}
		ok++
		_ = s.store.IncrementImportProgress(ctx, importID, 1, 0)
	}
	status := domain.ImportCompleted
	switch {
	case ok == 0 && failed > 0:
		status = domain.ImportFailed
	case failed > 0:
		status = domain.ImportPartial
	}
	return s.store.UpdateImport(ctx, importID, postgres.ImportUpdate{Status: &status, Completed: true})
}

// commitItem persists one normalized conversation with full provenance.
func (s *Service) commitItem(ctx context.Context, a Actor, im *domain.Import, it *domain.ImportItem, o commitItemOpt, extract bool) (*domain.Conversation, *domain.Idea, []domain.KnowledgeItem, error) {
	var conv imports.NormalizedConversation
	if err := json.Unmarshal(it.Payload, &conv); err != nil {
		return nil, nil, nil, fmt.Errorf("decode item: %w", err)
	}
	if len(conv.Messages) == 0 {
		return nil, nil, nil, errors.New("conversation has no messages")
	}
	kind := domain.SourceImport
	switch im.SourceKind {
	case "url":
		kind = domain.SourceURL
	case "paste":
		kind = domain.SourcePaste
	}
	title := strings.TrimSpace(conv.Title)
	if title == "" {
		title = trimTo(firstLine(conv.Messages[0].Content), 80)
	}
	var created *domain.Conversation
	var msgs []ExtractMessage
	err := s.store.WithTx(ctx, func(tx *postgres.Store) error {
		src := &domain.Source{UserID: a.UserID, Kind: kind, Provider: conv.Provider, Adapter: conv.Adapter, Title: title, URI: firstNonEmptyStr(conv.URL, im.URI),
			ExternalID: conv.ExternalID, ContentHash: it.ContentHash, Trusted: false,
			Metadata: map[string]any{"import_id": im.ID.String(), "import_item_id": it.ID.String(), "filename": im.Filename, "warnings": conv.Warnings,
				"injection_flags": it.InjectionFlags, "kind": conv.Kind}}
		if err := tx.CreateSource(ctx, src); err != nil {
			return err
		}
		c := &domain.Conversation{UserID: a.UserID, SourceID: &src.ID, Title: title, Origin: domain.OriginImported, Provider: conv.Provider, ExternalID: conv.ExternalID}
		if conv.CreatedAt != nil {
			c.StartedAt = *conv.CreatedAt
		}
		if it.Status == "DUPLICATE" || it.DuplicateOf != nil {
			c.ExternalID = "" // allow an explicit re-import without violating the external-id uniqueness
		}
		if err := tx.CreateConversation(ctx, c); err != nil {
			return err
		}
		base := c.StartedAt
		for i, m := range conv.Messages {
			role := domain.MessageRole(m.Role)
			if role != domain.RoleUser && role != domain.RoleAssistant && role != domain.RoleSystem {
				role = domain.RoleUser
			}
			msg := &domain.Message{UserID: a.UserID, ConversationID: c.ID, Role: role, Content: m.Content, Untrusted: true, ExternalID: m.ExternalID, Model: m.Model,
				Metadata: map[string]any{"imported": true, "provider": conv.Provider}}
			if m.CreatedAt != nil {
				msg.CreatedAt = *m.CreatedAt
			} else {
				msg.CreatedAt = base.Add(time.Duration(i) * time.Second)
			}
			if err := tx.AppendMessage(ctx, msg); err != nil {
				return err
			}
			id := msg.ID
			msgs = append(msgs, ExtractMessage{Index: i, Role: string(role), Content: m.Content, MessageID: &id})
		}
		created = c
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	var idea *domain.Idea
	switch {
	case o.IdeaID != nil:
		idea, err = s.store.GetIdea(ctx, a.UserID, *o.IdeaID)
		if err != nil {
			return created, nil, nil, err
		}
		if err := s.store.AttachConversation(ctx, a.UserID, created.ID, &idea.ID, idea.DefaultBranchID); err != nil {
			return created, nil, nil, err
		}
		s.activity(ctx, s.store, a, &idea.ID, idea.DefaultBranchID, "conversation.imported", domain.EntityConversation, &created.ID,
			fmt.Sprintf("Imported %s conversation: %s", conv.Provider, title), map[string]any{"import_id": im.ID.String()})
	case o.NewIdea:
		origin := ""
		for _, m := range conv.Messages {
			if m.Role == imports.RoleUser {
				origin = m.Content
				break
			}
		}
		ititle := strings.TrimSpace(o.NewIdeaTitle)
		if ititle == "" || placeholderTitle(ititle) {
			ititle = title
		}
		res, err := s.CreateIdea(ctx, a, CreateIdeaInput{Title: ititle, OriginText: trimTo(origin, 4000), ConversationID: &created.ID, SourceID: created.SourceID})
		if err != nil {
			return created, nil, nil, err
		}
		idea = res.Idea
	}
	var proposals []domain.KnowledgeItem
	if extract && idea != nil {
		ex, err := s.ExtractKnowledge(ctx, a.UserID, &idea.ID, idea.Title, msgs)
		if err == nil {
			proposals, _ = s.PersistProposals(ctx, a, idea.ID, idea.DefaultBranchID, &created.ID, created.SourceID, msgs, ex.Items)
			if idea.Summary == "" && ex.Summary != "" {
				sum := ex.Summary
				if u, err := s.UpdateIdea(ctx, a, idea.ID, UpdateIdeaInput{Summary: &sum, Reason: "Summary interpreted from imported conversation"}); err == nil {
					idea = u
				}
			}
			if ex.Summary != "" {
				sum := trimTo(ex.Summary, 1000)
				_ = s.store.UpdateConversationMeta(ctx, a.UserID, created.ID, nil, &sum)
			}
		}
	}
	upd := postgres.ImportItemUpdate{Status: ptr("IMPORTED"), ConversationID: &created.ID, ExtractedCount: ptr(len(proposals))}
	if idea != nil {
		upd.IdeaID = &idea.ID
	}
	if err := s.store.UpdateImportItem(ctx, it.ID, upd); err != nil {
		return created, idea, proposals, err
	}
	uid := a.UserID
	_, _ = s.store.EnqueueJob(ctx, &uid, JobEmbedConversation, EmbedJob{UserID: a.UserID, EntityType: domain.EntityConversation, EntityID: created.ID}, "embed-conv:"+created.ID.String(), time.Now().Add(time.Second))
	return created, idea, proposals, nil
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ImportNowInput imports synchronously (used by the agent and quick-paste flows).
type ImportNowInput struct {
	CreateImportInput
	IdeaID       *uuid.UUID `json:"idea_id,omitempty"`
	NewIdea      bool       `json:"new_idea,omitempty"`
	NewIdeaTitle string     `json:"new_idea_title,omitempty"`
	Extract      bool       `json:"extract"`
}

// ImportNowResult is the outcome of a synchronous import.
type ImportNowResult struct {
	Import         *domain.Import         `json:"import"`
	Conversations  []domain.Conversation  `json:"conversations"`
	Ideas          []domain.Idea          `json:"ideas"`
	Proposals      []domain.KnowledgeItem `json:"proposals"`
	Duplicates     []domain.Conversation  `json:"duplicates"`
	Warnings       []string               `json:"warnings"`
	InjectionFlags []string               `json:"injection_flags"`
}

// ImportNow parses and commits small inputs immediately (max 5 conversations), keeping full provenance.
func (s *Service) ImportNow(ctx context.Context, a Actor, in ImportNowInput) (*ImportNowResult, error) {
	if err := in.CreateImportInput.normalize(s.maxUpload()); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(append(append([]byte{}, in.Data...), []byte(in.URL)...))
	im := &domain.Import{SourceKind: in.SourceKind, Adapter: in.Adapter, Filename: in.Filename, URI: in.URL, ByteSize: int64(len(in.Data)),
		ContentHash: hex.EncodeToString(sum[:]), Options: map[string]any{"sync": true}, TargetIdeaID: in.IdeaID}
	if err := s.store.CreateImport(ctx, a.UserID, im, in.Data); err != nil {
		return nil, err
	}
	res, err := s.parseImport(ctx, a.UserID, im)
	if err != nil {
		fail := domain.ImportFailed
		msg := userFacingImportError(err)
		_ = s.store.UpdateImport(ctx, im.ID, postgres.ImportUpdate{Status: &fail, Error: &msg, ClearRaw: true, Completed: true})
		return nil, domain.Invalid("import", msg)
	}
	if len(res.Conversations) > 5 {
		fail := domain.ImportFailed
		msg := "This input contains many conversations; use the Import Center to preview and select them."
		_ = s.store.UpdateImport(ctx, im.ID, postgres.ImportUpdate{Status: &fail, Error: &msg, ClearRaw: true, Completed: true})
		return nil, domain.Invalid("import", msg)
	}
	if _, _, err := s.stageItems(ctx, a.UserID, im.ID, res); err != nil {
		return nil, err
	}
	items, err := s.store.ListImportItems(ctx, a.UserID, im.ID, true, nil)
	if err != nil {
		return nil, err
	}
	out := &ImportNowResult{Warnings: res.Warnings}
	flagSet := map[string]bool{}
	ok, failed := 0, 0
	for i := range items {
		it := items[i]
		for _, f := range it.InjectionFlags {
			flagSet[f] = true
		}
		if it.Status == "FAILED" {
			failed++
			continue
		}
		if it.Status == "DUPLICATE" && it.DuplicateOf != nil {
			dup, err := s.store.GetConversation(ctx, a.UserID, *it.DuplicateOf)
			if err == nil {
				out.Duplicates = append(out.Duplicates, *dup)
				if in.IdeaID != nil && dup.IdeaID == nil {
					idea, err := s.store.GetIdea(ctx, a.UserID, *in.IdeaID)
					if err == nil {
						_ = s.store.AttachConversation(ctx, a.UserID, dup.ID, &idea.ID, idea.DefaultBranchID)
					}
				}
			}
			skip := "SKIPPED"
			_ = s.store.UpdateImportItem(ctx, it.ID, postgres.ImportItemUpdate{Status: &skip})
			continue
		}
		conv, idea, props, err := s.commitItem(ctx, a, im, &it, commitItemOpt{IdeaID: in.IdeaID, NewIdea: in.NewIdea && in.IdeaID == nil, NewIdeaTitle: in.NewIdeaTitle}, in.Extract)
		if err != nil {
			failed++
			continue
		}
		ok++
		out.Conversations = append(out.Conversations, *conv)
		if idea != nil {
			out.Ideas = append(out.Ideas, *idea)
		}
		out.Proposals = append(out.Proposals, props...)
	}
	for f := range flagSet {
		out.InjectionFlags = append(out.InjectionFlags, f)
	}
	status := domain.ImportCompleted
	if ok == 0 && failed > 0 {
		status = domain.ImportFailed
	} else if failed > 0 {
		status = domain.ImportPartial
	}
	total := len(items)
	_ = s.store.UpdateImport(ctx, im.ID, postgres.ImportUpdate{Status: &status, Provider: &res.Provider, Adapter: &res.Adapter, TotalItems: &total,
		ProcessedItems: &ok, FailedItems: &failed, ClearRaw: true, Completed: true, Warnings: nonNil(res.Warnings)})
	out.Import, _ = s.store.GetImport(ctx, a.UserID, im.ID)
	return out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ParsePreview parses input without persisting anything (used for external-delta analysis).
func (s *Service) ParsePreview(ctx context.Context, in CreateImportInput) (*imports.ParseResult, error) {
	if err := in.normalize(s.maxUpload()); err != nil {
		return nil, err
	}
	if s.parser == nil {
		return nil, domain.Unavailable("import adapters are not configured")
	}
	if in.SourceKind == "url" {
		res, err := s.parser.ParseURL(ctx, s.fetcher, in.URL)
		if err != nil {
			return nil, domain.Invalid("url", userFacingImportError(err))
		}
		return res, nil
	}
	res, err := s.parser.Parse(ctx, imports.Input{Filename: in.Filename, Data: in.Data}, in.Adapter)
	if err != nil {
		return nil, domain.Invalid("input", userFacingImportError(err))
	}
	return res, nil
}

var placeholderTitleRe = regexp.MustCompile(`(?i)^(an? )?(new|untitled|imported)( idea| conversation| chat)*( from (an? |the )?(shared|imported|external)? ?(conversation|chat|link))?$|^(shared|imported) (conversation|chat)$`)

// placeholderTitle reports titles that say nothing about the idea ("New idea from shared
// conversation"); the conversation's own title is better.
func placeholderTitle(s string) bool { return placeholderTitleRe.MatchString(strings.TrimSpace(s)) }
