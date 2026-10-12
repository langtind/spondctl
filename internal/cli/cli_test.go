package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/langtind/ctlkit"
	"github.com/langtind/ctlkit/ctltest"
	"github.com/langtind/ctlkit/keystore"

	"github.com/langtind/spondctl/internal/spond"
)

// Every name and id below is made up.
const (
	groupsBody = `[
	  {"id":"G1","name":"Testvik IL G12","members":[
	    {"id":"M1","firstName":"Ola","lastName":"Testvik","guardians":[{"id":"P1","firstName":"Kari","lastName":"Testvik"}]},
	    {"id":"M2","firstName":"Nora","lastName":"Eksempel","guardians":[]}
	  ]},
	  {"id":"G2","name":"Testvik korps","members":[]}
	]`

	trainingBody = `{"id":"E1","heading":"Trening","description":"Husk leggskinn.\nVi er ute.",
	  "startTimestamp":"2026-10-14T16:00:00Z","endTimestamp":"2026-10-14T17:30:00Z","meetupTimestamp":"2026-10-14T15:45:00Z",
	  "location":{"feature":"Testvik kunstgress","address":"Banevegen 1"},
	  "responses":{"acceptedIds":["M2"],"declinedIds":[],"unansweredIds":["M1"],"waitinglistIds":[],"unconfirmedIds":[]}}`

	matchBody = `{"id":"E2","heading":"Kamp","cancelled":true,
	  "startTimestamp":"2026-10-17T10:00:00Z","endTimestamp":"2026-10-17T12:00:00Z",
	  "responses":{"acceptedIds":[],"declinedIds":["M2"],"unansweredIds":["M1"],"waitinglistIds":["M9"],"unconfirmedIds":[]}}`

	// {origin} becomes the address of the fake service.
	concertBody = `{"id":"E3","heading":"Konsert","startTimestamp":"2026-10-15T08:00:00Z","endTimestamp":"2026-10-15T12:00:00Z",
	  "attachments":[
	    {"id":"A1","title":"Søknad om fri","media":"{origin}/storage/upload/X1/S%C3%B8knad_om_fri.pdf","type":2,"timestamp":"2026-10-06T11:22:06.343Z"},
	    {"id":"A2","title":"Elsewhere","media":"https://files.example.invalid/storage/evil.pdf"},
	    {"id":"A3","title":"Climbing","media":"{origin}/storage/upload/X1/.."}
	  ],
	  "responses":{"acceptedIds":["M1"],"declinedIds":[],"unansweredIds":[],"waitinglistIds":[],"unconfirmedIds":[]}}`

	afterAccept = `{"acceptedIds":["M2","M1"],"declinedIds":[],"unansweredIds":[],"waitinglistIds":[],"unconfirmedIds":[]}`
)

// call is one request spondctl sent to the fake service.
type call struct {
	Method string
	Path   string
	Query  url.Values
	Token  string
	Body   string
}

// harness runs spondctl against a fake Spond, with the token in memory, on
// a fixed clock two hours east of UTC.
type harness struct {
	t      *testing.T
	tokens *keystore.Memory
	env    map[string]string
	calls  []call

	// answer is what the fake service says.
	answer func(c call) (int, string)
}

var (
	testZone = time.FixedZone("CEST", 2*60*60)
	testTime = time.Date(2026, 10, 12, 15, 0, 0, 0, testZone)
)

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, tokens: &keystore.Memory{Secret: "stored-token"}, env: map[string]string{}}
	h.answer = func(c call) (int, string) {
		switch {
		case c.Method == http.MethodPost && c.Path == "/auth2/login":
			return http.StatusOK, `{"accessToken":{"token":"new-token","expiration":"2027-01-01T00:00:00Z"}}`
		case c.Method == http.MethodGet && c.Path == "/groups/":
			return http.StatusOK, groupsBody
		case c.Method == http.MethodGet && c.Path == "/sponds/":
			// Later first, to show that spondctl sorts.
			return http.StatusOK, "[" + matchBody + "," + trainingBody + "]"
		case c.Method == http.MethodGet && c.Path == "/sponds/E1":
			return http.StatusOK, trainingBody
		case c.Method == http.MethodGet && c.Path == "/sponds/E3":
			return http.StatusOK, concertBody
		case c.Method == http.MethodGet && c.Path == "/storage/upload/X1/Søknad_om_fri.pdf":
			return http.StatusOK, "%PDF-1.4 made up"
		case c.Method == http.MethodPut && c.Path == "/sponds/E1/responses/M1":
			return http.StatusOK, afterAccept
		}
		return http.StatusNotFound, `{"message":"Not found"}`
	}
	return h
}

// program returns spondctl wired to the fake service. It works without a
// server too, which is what the contract test needs.
func (h *harness) program(baseURL string, client *http.Client) *ctlkit.Program {
	app := &App{
		Client: &spond.Client{HTTP: client, BaseURL: baseURL, MediaOrigin: baseURL},
		Tokens: h.tokens,
		Getenv: func(name string) string { return h.env[name] },
		Now:    func() time.Time { return testTime },
		Zone:   testZone,
	}
	return app.program(ctlkit.Build{Version: "1.2.3"}, os.DirFS("../../skills/"+Name))
}

func (h *harness) run(stdin string, args ...string) (code int, stdout, stderr string) {
	h.t.Helper()
	h.calls = nil
	var origin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c := call{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Token:  strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
			Body:   string(body),
		}
		if got := r.Header.Get("User-Agent"); got != Name+"/1.2.3 (+https://github.com/langtind/"+Name+")" {
			h.t.Errorf("User-Agent = %q", got)
		}
		h.calls = append(h.calls, c)
		status, answer := h.answer(c)
		w.WriteHeader(status)
		fmt.Fprint(w, strings.ReplaceAll(answer, "{origin}", origin))
	}))
	defer srv.Close()
	origin = srv.URL
	return ctltest.Run(h.program(srv.URL, srv.Client()), stdin, args...)
}

// sent lists the requests as "METHOD path".
func (h *harness) sent() string {
	parts := make([]string, len(h.calls))
	for i, c := range h.calls {
		parts[i] = c.Method + " " + c.Path
	}
	return strings.Join(parts, ", ")
}

func TestContract(t *testing.T) {
	build := func() *ctlkit.Program { return newHarness(t).program("http://127.0.0.1:0", http.DefaultClient) }
	ctltest.Contract(t, build, "../../skills/"+Name+"/SKILL.md", "../../README.md")
}

func TestGroups(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("", "groups")
	want := "# Groups\n\n- Testvik IL G12\n  Members: 2\n  id: G1\n- Testvik korps\n  Members: 0\n  id: G2\n"
	if code != 0 || stdout != want || stderr != "" || h.sent() != "GET /groups/" || h.calls[0].Token != "stored-token" {
		t.Errorf("exit = %d, stdout = %q, stderr = %q, sent = %s", code, stdout, stderr, h.sent())
	}

	_, stdout, _ = h.run("", "groups", "--json", "--fields", "id")
	if got := strings.Join(strings.Fields(stdout), ""); got != `[{"id":"G1"},{"id":"G2"}]` {
		t.Errorf("--fields id: %s", got)
	}
}

func TestMembers(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run("", "members", "G1")
	want := "# Members of Testvik IL G12\n\n- Ola Testvik\n  Guardians: Kari Testvik\n  id: M1\n- Nora Eksempel\n  id: M2\n"
	if code != 0 || stdout != want {
		t.Errorf("exit = %d, stdout = %q", code, stdout)
	}

	_, stdout, _ = h.run("", "members", "G1", "--json")
	var members []memberView
	ctltest.OneJSON(t, stdout, &members)
	if len(members) != 2 || members[0].Guardians[0] != (person{ID: "P1", Name: "Kari Testvik"}) || members[1].Guardians == nil {
		t.Errorf("members = %+v", members)
	}

	code, _, stderr := h.run("", "members", "G9")
	if code != 3 || stderr != "Error: the account is in no group with the id \"G9\"\nHint: run \"spondctl groups\" to see the ids.\n" {
		t.Errorf("unknown group: exit = %d, stderr = %q", code, stderr)
	}
}

func TestEventsAreTheUpcomingOnesInOrder(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("", "events")
	want := "# Events\n\n" +
		"- Trening\n  Start: 2026-10-14T18:00:00+02:00\n  End: 2026-10-14T19:30:00+02:00\n  Meet: 2026-10-14T17:45:00+02:00\n" +
		"  Place: Testvik kunstgress, Banevegen 1\n  Answers: 1 accepted, 0 declined, 1 unanswered\n  id: E1\n" +
		"- Kamp (cancelled)\n  Start: 2026-10-17T12:00:00+02:00\n  End: 2026-10-17T14:00:00+02:00\n" +
		"  Answers: 0 accepted, 1 declined, 1 unanswered, 1 on the waiting list\n  id: E2\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Errorf("exit = %d, stderr = %q, stdout:\n%s", code, stderr, stdout)
	}

	// Today starts at midnight in the user's zone, which is 22:00 UTC the day before.
	q := h.calls[0].Query
	if h.sent() != "GET /sponds/" || q.Get("minEndTimestamp") != "2026-10-11T22:00:00.000Z" || q.Get("max") != "21" || q.Get("scheduled") != "false" || q.Get("order") != "asc" || q.Has("maxStartTimestamp") || q.Has("groupId") {
		t.Errorf("sent = %s, query = %v", h.sent(), q)
	}
}

func TestEventsTakesAPeriodAndAGroup(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("", "events", "--group", "G1", "--from", "2026-11-01", "--to", "2026-11-30", "--limit", "5")
	q := h.calls[0].Query
	if code != 0 || q.Get("groupId") != "G1" || q.Get("minEndTimestamp") != "2026-10-31T22:00:00.000Z" || q.Get("maxStartTimestamp") != "2026-11-30T22:00:00.000Z" || q.Get("max") != "6" {
		t.Errorf("exit = %d, stderr = %q, query = %v", code, stderr, q)
	}

	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"events", "--from", "1. november"}, `--from takes a date as yyyy-mm-dd, not "1. november"`},
		{[]string{"events", "--from", "2026-11-02", "--to", "2026-11-01"}, "--to 2026-11-01 is before --from 2026-11-02"},
		{[]string{"events", "--group", "G1?max=5"}, `"G1?max=5" cannot be the group id`},
		{[]string{"events", "--limit", "100"}, "--limit takes 1 to 99, not 100"},
	} {
		code, _, stderr := h.run("", tt.args...)
		if code != 2 || !strings.Contains(stderr, tt.want) || len(h.calls) != 0 {
			t.Errorf("%v: exit = %d, stderr = %q, sent = %s", tt.args, code, stderr, h.sent())
		}
	}
}

func TestEventsSaysWhenTheListIsCutOff(t *testing.T) {
	h := newHarness(t)
	_, stdout, _ := h.run("", "events", "--limit", "1", "--json", "--fields", "id,responses")
	var list struct {
		Items     []map[string]any `json:"items"`
		Truncated bool             `json:"truncated"`
	}
	ctltest.OneJSON(t, stdout, &list)
	if !list.Truncated || len(list.Items) != 1 || len(list.Items[0]) != 2 || list.Items[0]["id"] != "E1" {
		t.Errorf("list = %+v", list)
	}

	_, stdout, _ = h.run("", "events", "--limit", "1")
	if !strings.HasSuffix(stdout, "\nThere are more. Raise --limit or narrow the dates to see them.\n") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestNoEventsIsAnEmptyList(t *testing.T) {
	h := newHarness(t)
	h.answer = func(call) (int, string) { return http.StatusOK, `[]` }
	_, stdout, _ := h.run("", "events", "--json")
	if got := strings.Join(strings.Fields(stdout), ""); got != `{"items":[],"truncated":false}` {
		t.Errorf("stdout = %s", got)
	}
}

func TestEventNamesWhoAnswered(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("", "event", "E1")
	want := "# Trening\n\nStart: 2026-10-14T18:00:00+02:00\nEnd: 2026-10-14T19:30:00+02:00\nMeet: 2026-10-14T17:45:00+02:00\n" +
		"Place: Testvik kunstgress, Banevegen 1\nid: E1\n\n## Description\n\nHusk leggskinn.\nVi er ute.\n\n## Answers\n\n" +
		"Accepted (1):\n- Nora Eksempel (id M2)\n\nUnanswered (1):\n- Ola Testvik (id M1)\n"
	if code != 0 || stdout != want || stderr != "" || h.sent() != "GET /sponds/E1, GET /groups/" {
		t.Errorf("exit = %d, stderr = %q, sent = %s, stdout:\n%s", code, stderr, h.sent(), stdout)
	}

	_, stdout, _ = h.run("", "event", "E1", "--json")
	var view eventView
	ctltest.OneJSON(t, stdout, &view)
	if view.Start != "2026-10-14T16:00:00Z" || view.Responses.Unanswered[0] != (person{ID: "M1", Name: "Ola Testvik"}) || view.Responses.Declined == nil {
		t.Errorf("view = %+v", view)
	}
}

func TestAnUnknownEventIsNotFound(t *testing.T) {
	code, stdout, _ := newHarness(t).run("", "event", "E9", "--json")
	var doc ctltest.ErrorDocument
	ctltest.OneJSON(t, stdout, &doc)
	if code != 3 || doc.Error.Kind != "not_found" || doc.Error.Message != "Spond: HTTP 404: Not found" || doc.Error.Hint != `run "spondctl events" to see the ids.` {
		t.Errorf("exit = %d, error = %+v", code, doc.Error)
	}
}

func TestEventListsItsAttachments(t *testing.T) {
	h := newHarness(t)
	_, stdout, _ := h.run("", "event", "E3")
	if !strings.Contains(stdout, "\n## Attachments\n\n- Søknad om fri (id A1)\n- Elsewhere (id A2)\n- Climbing (id A3)\n\n## Answers\n") {
		t.Errorf("stdout:\n%s", stdout)
	}

	_, stdout, _ = h.run("", "event", "E1", "--json", "--fields", "attachments")
	if got := strings.Join(strings.Fields(stdout), ""); got != `{"attachments":[]}` {
		t.Errorf("an event without attachments: %s", got)
	}

	_, stdout, _ = h.run("", "event", "E3", "--raw")
	var raw map[string]any
	ctltest.OneJSON(t, stdout, &raw)
	if raw["heading"] != "Konsert" || len(raw["attachments"].([]any)) != 3 {
		t.Errorf("--raw = %v", raw)
	}
}

func TestAttachmentIsDownloadedWithoutTheToken(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	code, stdout, stderr := h.run("", "attachment", "E3", "A1", "--out", dir, "--json")
	var result attachmentResult
	ctltest.OneJSON(t, stdout, &result)
	want := filepath.Join(dir, "Søknad_om_fri.pdf")
	if code != 0 || result.Path != want || result.Bytes != 16 || result.Title != "Søknad om fri" {
		t.Fatalf("exit = %d, stderr = %q, result = %+v", code, stderr, result)
	}
	if data, err := os.ReadFile(want); err != nil || string(data) != "%PDF-1.4 made up" {
		t.Errorf("file = %q, %v", data, err)
	}
	if download := h.calls[len(h.calls)-1]; h.sent() != "GET /sponds/E3, GET /storage/upload/X1/Søknad_om_fri.pdf" || download.Token != "" {
		t.Errorf("sent = %s, token on the download = %q", h.sent(), download.Token)
	}

	// The file is there now, so a second run needs --replace.
	code, _, stderr = h.run("", "attachment", "E3", "A1", "--out", dir)
	if code != 5 || !strings.Contains(stderr, "is already there") || h.sent() != "GET /sponds/E3" {
		t.Errorf("second run: exit = %d, stderr = %q, sent = %s", code, stderr, h.sent())
	}
	if code, _, _ := h.run("", "attachment", "E3", "A1", "--out", dir, "--replace"); code != 0 {
		t.Errorf("with --replace: exit = %d", code)
	}

	named := filepath.Join(dir, "fri.pdf")
	if code, stdout, _ := h.run("", "attachment", "E3", "A1", "--out", named); code != 0 || !strings.Contains(stdout, named) {
		t.Errorf("--out with a file name: exit = %d, stdout = %q", code, stdout)
	}
}

func TestAttachmentFetchesOnlyFromSpond(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	code, _, stderr := h.run("", "attachment", "E3", "A2", "--out", dir)
	if code != 1 || !strings.Contains(stderr, `"https://files.example.invalid/storage/evil.pdf", which is not under`) || h.sent() != "GET /sponds/E3" {
		t.Errorf("another origin: exit = %d, stderr = %q, sent = %s", code, stderr, h.sent())
	}

	// A name that climbs out of the directory is not used.
	h.run("", "attachment", "E3", "A3", "--out", dir)
	if entries, _ := os.ReadDir(filepath.Dir(dir)); len(entries) != 1 {
		t.Errorf("something was written beside the directory: %v", entries)
	}

	code, _, stderr = h.run("", "attachment", "E3", "A9", "--out", dir)
	if code != 3 || !strings.Contains(stderr, `the event "Konsert" has no attachment with the id "A9"`) || !strings.Contains(stderr, `run "spondctl event E3" to see the 3 it has.`) {
		t.Errorf("unknown attachment: exit = %d, stderr = %q", code, stderr)
	}
}

func TestARespondDryRunReadsButChangesNothing(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run("", "respond", "E1", "M1", "accept", "--dry-run", "--json")
	var result respondResult
	ctltest.OneJSON(t, stdout, &result)
	if code != 0 || !result.DryRun || result.Event.Heading != "Trening" || result.Member.Name != "Ola Testvik" || result.Request.Accepted != "true" || result.Responses != nil {
		t.Errorf("exit = %d, result = %+v", code, result)
	}
	if h.sent() != "GET /sponds/E1, GET /groups/" {
		t.Errorf("a dry run sent: %s", h.sent())
	}

	_, stdout, _ = h.run("", "respond", "E1", "M1", "decline", "--dry-run")
	if stdout != "Dry run. Would decline \"Trening\" at 2026-10-14T18:00:00+02:00 for Ola Testvik. Nothing was changed.\n" {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestRespondNeedsAYes(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("", "respond", "E1", "M1", "accept")
	if code != 2 || stdout != "" || !strings.Contains(stderr, `refusing without confirmation: Accept "Trening" at 2026-10-14T18:00:00+02:00 for Ola Testvik?`) || strings.Contains(h.sent(), "PUT") {
		t.Errorf("without --yes: exit = %d, stderr = %q, sent = %s", code, stderr, h.sent())
	}

	code, stdout, _ = h.run("", "respond", "E1", "M1", "accept", "--yes")
	want := "Done: accept \"Trening\" at 2026-10-14T18:00:00+02:00 for Ola Testvik.\nAnswers now: 2 accepted, 0 declined, 0 unanswered.\n"
	last := h.calls[len(h.calls)-1]
	if code != 0 || stdout != want || h.sent() != "GET /sponds/E1, GET /groups/, PUT /sponds/E1/responses/M1" || last.Body != `{"accepted":"true"}` {
		t.Errorf("with --yes: exit = %d, stdout = %q, sent = %s, body = %s", code, stdout, h.sent(), last.Body)
	}

	h.run("", "respond", "E1", "M1", "DECLINE", "--yes")
	if last := h.calls[len(h.calls)-1]; last.Body != `{"accepted":"false"}` {
		t.Errorf("decline body = %s", last.Body)
	}
}

func TestRespondRefusesWhatItCannotName(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("", "respond", "E1", "M1", "maybe", "--yes")
	if code != 2 || !strings.Contains(stderr, `the answer "maybe" is not one of: accept, decline`) || len(h.calls) != 0 {
		t.Errorf("wrong answer: exit = %d, stderr = %q, sent = %s", code, stderr, h.sent())
	}

	code, _, stderr = h.run("", "respond", "E1", "M7", "accept", "--yes")
	if code != 3 || !strings.Contains(stderr, `no group of the account has a member with the id "M7"`) || strings.Contains(h.sent(), "PUT") {
		t.Errorf("unknown member: exit = %d, stderr = %q, sent = %s", code, stderr, h.sent())
	}

	// Nora is a member, and the fake service answers 404 for her on this
	// event. She is among the invited, so no note is due.
	code, _, stderr = h.run("", "respond", "E1", "M2", "decline", "--yes")
	if code != 3 || strings.Contains(stderr, "Note:") {
		t.Errorf("refused by the service: exit = %d, stderr = %q", code, stderr)
	}
}

func TestWithoutATokenNothingIsSent(t *testing.T) {
	h := newHarness(t)
	h.tokens.Secret = ""
	code, _, stderr := h.run("", "events")
	if code != 4 || stderr != "Error: not logged in\nHint: run \"spondctl login\", or set SPONDCTL_TOKEN.\n" || len(h.calls) != 0 {
		t.Errorf("exit = %d, stderr = %q, sent = %s", code, stderr, h.sent())
	}

	h.env[envToken] = "env-token"
	if code, _, _ := h.run("", "groups"); code != 0 || h.calls[0].Token != "env-token" {
		t.Errorf("with %s: exit = %d, calls = %+v", envToken, code, h.calls)
	}
}

func TestATokenThatHasRunOutSaysToLogInAgain(t *testing.T) {
	h := newHarness(t)
	h.answer = func(call) (int, string) { return http.StatusUnauthorized, `` }
	code, _, stderr := h.run("", "groups")
	if code != 4 || stderr != "Error: Spond: HTTP 401\nHint: the token may have run out. Run \"spondctl login\" again.\n" {
		t.Errorf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestLoginStoresTheToken(t *testing.T) {
	h := newHarness(t)
	h.tokens.Secret = ""
	code, stdout, stderr := h.run("hunter2\n", "login", "--email", "kari@example.invalid", "--password-stdin")
	if code != 0 || stdout != "Logged in. The token is in the system keyring.\n" || h.tokens.Secret != "new-token" || h.calls[0].Token != "" {
		t.Errorf("exit = %d, stdout = %q, stderr = %q, token = %q", code, stdout, stderr, h.tokens.Secret)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(h.calls[0].Body), &body); err != nil || body["email"] != "kari@example.invalid" || body["password"] != "hunter2" {
		t.Errorf("login body = %q", h.calls[0].Body)
	}

	if code, _, stderr := h.run("hunter2\n", "login", "--password-stdin"); code != 2 || !strings.Contains(stderr, "--email is required") {
		t.Errorf("without --email: exit = %d, stderr = %q", code, stderr)
	}
}

func TestALoginWithoutATokenStoresNothing(t *testing.T) {
	for _, answer := range []struct {
		status int
		body   string
	}{
		{http.StatusUnauthorized, `{"errorKey":"invalidCredentials"}`},
		// An account with two-factor login is answered 200 without a token.
		{http.StatusOK, `{"phoneNumber":"+47 000 00 000","token":"a-challenge"}`},
	} {
		h := newHarness(t)
		h.tokens.Secret = ""
		h.answer = func(call) (int, string) { return answer.status, answer.body }
		code, stdout, stderr := h.run("nope\n", "login", "--email", "kari@example.invalid", "--password-stdin")
		if code != 4 || h.tokens.Secret != "" || !strings.Contains(stderr, "Hint: check the email and the password.") || strings.Contains(stdout+stderr, "000") || strings.Contains(stdout+stderr, "a-challenge") {
			t.Errorf("HTTP %d: exit = %d, stdout = %q, stderr = %q, token = %q", answer.status, code, stdout, stderr, h.tokens.Secret)
		}
	}
}

func TestLogoutRemovesTheToken(t *testing.T) {
	h := newHarness(t)
	if code, _, _ := h.run("", "logout"); code != 0 || h.tokens.Secret != "" {
		t.Errorf("exit = %d, token = %q", code, h.tokens.Secret)
	}
}
