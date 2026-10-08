package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/domain"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/adapters"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/service"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/testutil"
)

// importAndDrain starts an import and runs the parse job.
func importAndDrain(t *testing.T, e *testutil.Env, u *testutil.User, in service.CreateImportInput) *service.ImportDetail {
	t.Helper()
	im := try(e.Svc().CreateImport(ctx(), u.Actor(), in)).must(t)
	if im.Status != domain.ImportQueued {
		t.Fatalf("new imports are queued, got %s", im.Status)
	}
	e.DrainJobs(t)
	return try(e.Svc().GetImport(ctx(), u.ID, im.ID)).must(t)
}

func TestImportFixturesParseToPreview(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	tests := []struct {
		file      string
		provider  string
		adapter   string
		items     int
		firstName string
	}{
		{"imports/chatgpt/conversations.json", "chatgpt", "chatgpt/v1", 2, "Biker community trip platform"},
		{"imports/claude/conversations.json", "claude", "claude/v1", 2, "Clinic scheduling assistant"},
		{"imports/gemini/MyActivity.json", "gemini", "gemini/v1", -1, ""},
		{"imports/markdown/notes.md", "markdown", "markdown/v1", 1, "Clinic management platform: feature notes"},
		{"imports/markdown/chatgpt_copy.md", "", "", 1, ""},
		{"imports/json/messages.json", "json", "json/v1", 1, "Clinic onboarding checklist"},
		{"imports/text/transcript.txt", "", "", 1, ""},
		{"imports/injection/malicious_chatgpt.json", "chatgpt", "chatgpt/v1", 1, "Trip sharing notes"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			parts := strings.Split(tt.file, "/")
			data := testutil.Fixture(t, parts...)
			d := importAndDrain(t, e, u, service.CreateImportInput{SourceKind: "file", Filename: parts[len(parts)-1], Data: data})
			im := d.Import
			if im.Status != domain.ImportPreview {
				t.Fatalf("status = %s (%s)", im.Status, im.Error)
			}
			if tt.provider != "" && (im.Provider != tt.provider || im.Adapter != tt.adapter) {
				t.Errorf("provider/adapter = %s/%s, want %s/%s", im.Provider, im.Adapter, tt.provider, tt.adapter)
			}
			if tt.items > 0 && (len(d.Items) != tt.items || im.TotalItems != tt.items) {
				t.Errorf("items = %d (total %d), want %d", len(d.Items), im.TotalItems, tt.items)
			}
			if len(d.Items) == 0 {
				t.Fatal("no items parsed")
			}
			if tt.firstName != "" && d.Items[0].Title != tt.firstName {
				t.Errorf("first title = %q, want %q", d.Items[0].Title, tt.firstName)
			}
			for _, it := range d.Items {
				if it.Status != "PENDING" || it.ContentHash == "" || it.MessageCount == 0 || it.InjectionFlags == nil || len(it.Preview) == 0 {
					t.Errorf("item %q: %+v", it.Title, it)
				}
			}
			raw := try(e.Store().ImportRaw(ctx(), u.ID, im.ID)).must(t)
			if raw != nil {
				t.Error("the raw upload must be cleared after parsing")
			}
		})
	}
}

func TestImportCommitFlowWithExtractionAndDedupe(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	data := testutil.Fixture(t, "imports", "chatgpt", "conversations.json")
	d := importAndDrain(t, e, u, service.CreateImportInput{Filename: "conversations.json", Data: data})
	if d.Import.Status != domain.ImportPreview || d.Import.SourceKind != "file" || d.Import.ByteSize != int64(len(data)) {
		t.Fatalf("preview: %+v", d.Import)
	}
	bike, clinic := d.Items[0], d.Items[1]
	_, err := svc.CommitImport(ctx(), u.Actor(), d.Import.ID, service.CommitImportInput{Items: []service.CommitItem{{ItemID: uuid.New(), Include: true}}})
	wantKind(t, err, domain.KindInvalid)
	_, err = svc.CommitImport(ctx(), u.Actor(), d.Import.ID, service.CommitImportInput{Items: []service.CommitItem{{ItemID: bike.ID, Include: false}}})
	wantKind(t, err, domain.KindInvalid) // nothing selected
	im := try(svc.CommitImport(ctx(), u.Actor(), d.Import.ID, service.CommitImportInput{Extract: true, Items: []service.CommitItem{
		{ItemID: bike.ID, Include: true, NewIdea: true, NewIdeaTitle: "Biker Trips"}, {ItemID: clinic.ID, Include: false}}})).must(t)
	if im.Status != domain.ImportProcessing || im.Stage != "commit" || im.TotalItems != 1 {
		t.Fatalf("commit queued: %+v", im)
	}
	_, err = svc.CommitImport(ctx(), u.Actor(), d.Import.ID, service.CommitImportInput{Items: []service.CommitItem{{ItemID: bike.ID, Include: true}}})
	wantKind(t, err, domain.KindConflict) // only PREVIEW imports can be committed
	e.DrainJobs(t)
	d = try(svc.GetImport(ctx(), u.ID, d.Import.ID)).must(t)
	if d.Import.Status != domain.ImportCompleted || d.Import.ProcessedItems != 1 || d.Import.CompletedAt == nil {
		t.Fatalf("after commit: %+v", d.Import)
	}
	bike, clinic = d.Items[0], d.Items[1]
	if bike.Status != "IMPORTED" || bike.ConversationID == nil || bike.IdeaID == nil || clinic.Status != "SKIPPED" {
		t.Fatalf("items after commit: %+v / %+v", bike, clinic)
	}
	conv := try(st.GetConversation(ctx(), u.ID, *bike.ConversationID)).must(t)
	if conv.Origin != domain.OriginImported || conv.Provider != "chatgpt" || conv.IdeaID == nil || *conv.IdeaID != *bike.IdeaID || conv.SourceID == nil || conv.Summary == "" {
		t.Errorf("imported conversation: %+v", conv)
	}
	src := try(st.GetSource(ctx(), u.ID, *conv.SourceID)).must(t)
	if src.Trusted || src.Kind != domain.SourceImport || src.ContentHash != bike.ContentHash || src.Adapter != "chatgpt/v1" || src.Metadata["import_id"] != d.Import.ID.String() {
		t.Errorf("source provenance: %+v", src)
	}
	msgs := try(st.ListMessages(ctx(), u.ID, conv.ID, 0, 100)).must(t)
	if len(msgs) != bike.MessageCount || len(msgs) == 0 {
		t.Fatalf("messages = %d, want %d", len(msgs), bike.MessageCount)
	}
	for _, m := range msgs {
		if !m.Untrusted || m.Metadata["imported"] != true {
			t.Errorf("imported message must be untrusted data: %+v", m)
		}
	}
	idea := try(st.GetIdea(ctx(), u.ID, *bike.IdeaID)).must(t)
	if idea.Title != "Biker Trips" || idea.OriginConversationID == nil || *idea.OriginConversationID != conv.ID || idea.OriginText == "" || idea.Summary == "" {
		t.Errorf("idea from import: %+v", idea)
	}
	props := try(st.ListKnowledge(ctx(), postgres.KnowledgeFilter{UserID: u.ID, IdeaID: &idea.ID, ReviewStates: []domain.ReviewState{domain.ReviewProposed}})).must(t)
	if len(props) == 0 || bike.ExtractedCount != len(props) {
		t.Fatalf("extraction must create PROPOSED items: %d (extracted_count %d)", len(props), bike.ExtractedCount)
	}
	for _, p := range props {
		if p.SourceMessageID == nil || p.SourceConversationID == nil || *p.SourceConversationID != conv.ID || p.SourceID == nil || p.SourceExcerpt == "" ||
			p.CreatedBy != domain.ActorImport || p.Confidence == nil {
			t.Errorf("proposal provenance: %+v", p)
		}
		m := try(st.GetMessage(ctx(), u.ID, *p.SourceMessageID)).must(t)
		if m.ConversationID != conv.ID {
			t.Error("proposal points at a message of another conversation")
		}
	}
	if live := try(st.ListKnowledge(ctx(), postgres.KnowledgeFilter{UserID: u.ID, IdeaID: &idea.ID, LiveOnly: true})).must(t); len(live) != 0 {
		t.Errorf("imports must not create durable knowledge without review: %d live", len(live))
	}
	// Messages were embedded by the queued job.
	if countRows(t, `SELECT count(*) FROM embeddings WHERE entity_type = 'message' AND user_id = $1`, u.ID) == 0 {
		t.Error("imported messages should be embedded by the worker")
	}

	// Re-importing the same export flags the committed conversation as a duplicate.
	again := importAndDrain(t, e, u, service.CreateImportInput{Filename: "conversations.json", Data: data})
	if again.Items[0].Status != "DUPLICATE" || again.Items[0].DuplicateOf == nil || *again.Items[0].DuplicateOf != conv.ID {
		t.Errorf("re-import must detect the duplicate: %+v", again.Items[0])
	}
	if again.Items[1].Status != "PENDING" {
		t.Errorf("the skipped conversation is not a duplicate: %s", again.Items[1].Status)
	}
	_, err = svc.CommitImport(ctx(), u.Actor(), again.Import.ID, service.CommitImportInput{Items: []service.CommitItem{{ItemID: again.Items[0].ID, Include: true}}})
	wantKind(t, err, domain.KindInvalid) // duplicates are skipped unless explicitly re-imported
	// Explicit re-import of a duplicate into an existing idea is allowed.
	try(svc.CommitImport(ctx(), u.Actor(), again.Import.ID, service.CommitImportInput{IncludeDuplicates: true,
		Items: []service.CommitItem{{ItemID: again.Items[0].ID, Include: true, IdeaID: &idea.ID}}})).must(t)
	e.DrainJobs(t)
	final := try(svc.GetImport(ctx(), u.ID, again.Import.ID)).must(t)
	if final.Import.Status != domain.ImportCompleted || final.Items[0].Status != "IMPORTED" {
		t.Errorf("explicit duplicate re-import: %+v %+v", final.Import, final.Items[0])
	}
}

func TestImportInjectionIsFlaggedAndInert(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc, st := e.Svc(), e.Store()
	target := e.MustIdea(t, u, "Trip sharing", "trip sharing feature")
	ideasBefore := countRows(t, `SELECT count(*) FROM ideas WHERE user_id = $1`, u.ID)
	d := importAndDrain(t, e, u, service.CreateImportInput{Filename: "malicious.json", Data: testutil.Fixture(t, "imports", "injection", "malicious_chatgpt.json"), TargetIdeaID: &target.Idea.ID})
	flags := append([]string(nil), d.Items[0].InjectionFlags...)
	sort.Strings(flags)
	for _, want := range []string{"destructive_command", "ignore_instructions", "system_prompt_probe"} {
		if !contains(flags, want) {
			t.Errorf("injection flag %s missing: %v", want, flags)
		}
	}
	try(svc.CommitImport(ctx(), u.Actor(), d.Import.ID, service.CommitImportInput{Extract: true, Items: []service.CommitItem{{ItemID: d.Items[0].ID, Include: true}}})).must(t)
	e.DrainJobs(t)
	d = try(svc.GetImport(ctx(), u.ID, d.Import.ID)).must(t)
	if d.Import.Status != domain.ImportCompleted || d.Items[0].IdeaID == nil || *d.Items[0].IdeaID != target.Idea.ID {
		t.Fatalf("commit into the target idea: %+v %+v", d.Import, d.Items[0])
	}
	if n := countRows(t, `SELECT count(*) FROM ideas WHERE user_id = $1`, u.ID); n != ideasBefore {
		t.Errorf("an injected 'delete every idea' must not change the vault: %d → %d ideas", ideasBefore, n)
	}
	conv := try(st.GetConversation(ctx(), u.ID, *d.Items[0].ConversationID)).must(t)
	src := try(st.GetSource(ctx(), u.ID, *conv.SourceID)).must(t)
	if f, _ := src.Metadata["injection_flags"].([]any); len(f) < 3 {
		t.Errorf("source keeps the injection flags: %v", src.Metadata["injection_flags"])
	}
	for _, m := range try(st.ListMessages(ctx(), u.ID, conv.ID, 0, 10)).must(t) {
		if !m.Untrusted {
			t.Error("injected content must be stored as untrusted data")
		}
	}
	// Nothing extracted from the injection is durable.
	if live := try(st.ListKnowledge(ctx(), postgres.KnowledgeFilter{UserID: u.ID, IdeaID: &target.Idea.ID, LiveOnly: true})).must(t); len(live) != 0 {
		t.Errorf("injected content created durable knowledge: %+v", live)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestImportPrivateAndInvalidURLs(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	svc := e.Svc()
	d := importAndDrain(t, e, u, service.CreateImportInput{URL: "https://chatgpt.com/c/abc"})
	if d.Import.Status != domain.ImportFailed || d.Import.CompletedAt == nil {
		t.Fatalf("private URL import must fail: %+v", d.Import)
	}
	for _, want := range []string{"private conversation", "Share", "chatgpt.com/share", "Export data", "never ask for your password"} {
		if !strings.Contains(d.Import.Error, want) {
			t.Errorf("error should help the user (%q missing): %s", want, d.Import.Error)
		}
	}
	_, err := svc.ImportNow(ctx(), u.Actor(), service.ImportNowInput{CreateImportInput: service.CreateImportInput{URL: "https://claude.ai/chat/3f2a1b0c"}})
	wantKind(t, err, domain.KindInvalid)
	if !strings.Contains(err.Error(), "Claude") {
		t.Errorf("platform-specific help expected: %v", err)
	}
	_, err = svc.ParsePreview(ctx(), service.CreateImportInput{URL: "https://chatgpt.com/c/abc"})
	wantKind(t, err, domain.KindInvalid)
	for _, in := range []service.CreateImportInput{
		{SourceKind: "url", URL: "ftp://example.com/x"},
		{SourceKind: "url", URL: "javascript:alert(1)"},
		{SourceKind: "paste", Text: "   "},
		{SourceKind: "file"},
		{SourceKind: "carrier-pigeon", Text: "x"},
	} {
		if _, err := svc.CreateImport(ctx(), u.Actor(), in); domain.KindOf(err) != domain.KindInvalid {
			t.Errorf("CreateImport(%+v) = %v, want invalid", in.SourceKind, err)
		}
	}
	big := make([]byte, e.App.Config.MaxUploadBytes+1)
	if _, err := svc.CreateImport(ctx(), u.Actor(), service.CreateImportInput{SourceKind: "file", Filename: "x.json", Data: big}); domain.KindOf(err) != domain.KindInvalid {
		t.Errorf("oversized upload must be rejected: %v", err)
	}
	unsupported := importAndDrain(t, e, u, service.CreateImportInput{SourceKind: "file", Filename: "image.png", Data: []byte{0x89, 'P', 'N', 'G', 0, 0, 0, 0}})
	if unsupported.Import.Status != domain.ImportFailed || !strings.Contains(unsupported.Import.Error, "Unsupported format") {
		t.Errorf("binary input: %+v", unsupported.Import)
	}
	// Path traversal in filenames is neutralized.
	safe := try(svc.CreateImport(ctx(), u.Actor(), service.CreateImportInput{SourceKind: "file", Filename: "../../etc/notes.md", Data: []byte("# Notes\n\nhello world")})).must(t)
	if safe.Filename != "notes.md" {
		t.Errorf("filename = %q", safe.Filename)
	}
	e.DrainJobs(t)
}

// fakeFetcher serves canned public pages (no network).
type fakeFetcher struct{ pages map[string][]byte }

func (f fakeFetcher) Fetch(_ context.Context, rawURL string) ([]byte, string, string, error) {
	if b, ok := f.pages[rawURL]; ok {
		return b, "text/html; charset=utf-8", rawURL, nil
	}
	return nil, "", "", fmt.Errorf("page not found (HTTP 404)")
}

func TestImportPublicShareLinkWithFakeFetcher(t *testing.T) {
	e := requireEnv(t)
	u := e.NewUser(t)
	const share = "https://chatgpt.com/share/6a1f0c2e-5a5e-4d3e-9f10-aa11bb22cc03"
	svc := service.New(service.Deps{Store: e.Store(), Gateway: e.App.Gateway, Config: e.App.Config, Log: testutil.Logger(),
		Fetcher: fakeFetcher{pages: map[string][]byte{share: testutil.Fixture(t, "imports", "chatgpt", "share_page.html")}}, Parser: adapters.NewRegistry()})
	res := try(svc.ImportNow(ctx(), u.Actor(), service.ImportNowInput{CreateImportInput: service.CreateImportInput{URL: share}, NewIdea: true, Extract: true})).must(t)
	if len(res.Conversations) != 1 || len(res.Ideas) != 1 || res.Import.Status != domain.ImportCompleted || res.Import.Adapter != "chatgpt/v2" {
		t.Fatalf("share import: %+v", res.Import)
	}
	conv := res.Conversations[0]
	if conv.Title != "Weekend ride route voting" || conv.Provider != "chatgpt" {
		t.Errorf("conversation: %+v", conv)
	}
	src := try(e.Store().GetSource(ctx(), u.ID, *conv.SourceID)).must(t)
	if src.Kind != domain.SourceURL || src.URI != share || src.Trusted {
		t.Errorf("url source: %+v", src)
	}
	// Importing the same link again is recognised as a duplicate.
	again := try(svc.ImportNow(ctx(), u.Actor(), service.ImportNowInput{CreateImportInput: service.CreateImportInput{URL: share}, NewIdea: true})).must(t)
	if len(again.Conversations) != 0 || len(again.Duplicates) != 1 || again.Duplicates[0].ID != conv.ID {
		t.Errorf("duplicate share import: %+v", again)
	}
	if _, err := svc.ImportNow(ctx(), u.Actor(), service.ImportNowInput{CreateImportInput: service.CreateImportInput{URL: "https://chatgpt.com/share/does-not-exist"}}); domain.KindOf(err) != domain.KindInvalid {
		t.Errorf("unreachable link: %v", err)
	}
	b, _ := json.Marshal(res.Import)
	if strings.Contains(string(b), "raw_payload") {
		t.Error("import JSON must not expose raw payloads")
	}
}
