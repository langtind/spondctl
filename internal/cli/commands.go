package cli

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/langtind/ctlkit"

	"github.com/langtind/spondctl/internal/spond"
)

const (
	defaultLimit = 20
	dateLayout   = "2006-01-02"
)

var answers = []string{"accept", "decline"}

// commands is the command table, the single description of the command
// line. Add a command or a flag here and nowhere else.
func (a *App) commands() []ctlkit.Command {
	return []ctlkit.Command{
		{
			Name:    "login",
			Summary: "Log in and store the token in the system keyring",
			Flags: []ctlkit.Flag{
				{Name: "email", Kind: ctlkit.String, Value: "email", Help: "the email of the Spond account"},
				{Name: "password-stdin", Kind: ctlkit.Bool, Help: "read the password from stdin instead of prompting"},
			},
			Run: a.login,
		},
		{
			Name:    "logout",
			Summary: "Remove the stored token",
			Run:     a.logout,
		},
		{
			Name:    "groups",
			Summary: "List the groups the account belongs to",
			Flags:   []ctlkit.Flag{ctlkit.JSONFlag, ctlkit.FieldsFlag},
			Run:     a.groups,
		},
		{
			Name:    "members",
			Summary: "List the members of a group, with their guardians",
			Args:    []ctlkit.Arg{{Name: "group-id", Required: true, ID: true, Help: "the id, as groups prints it"}},
			Flags:   []ctlkit.Flag{ctlkit.JSONFlag, ctlkit.FieldsFlag},
			Run:     a.members,
		},
		{
			Name:    "events",
			Summary: "List events, the upcoming ones unless dates are given",
			Flags: []ctlkit.Flag{
				{Name: "group", Kind: ctlkit.String, Value: "group-id", Help: "only events in this group"},
				{Name: "from", Kind: ctlkit.String, Value: "yyyy-mm-dd", Help: "events that end on this date or later. Without the flag: today"},
				{Name: "to", Kind: ctlkit.String, Value: "yyyy-mm-dd", Help: "events that start on this date or earlier"},
				ctlkit.LimitFlag(defaultLimit, spond.MaxEvents-1),
				ctlkit.JSONFlag,
				ctlkit.FieldsFlag,
			},
			Run: a.events,
		},
		{
			Name:    "event",
			Summary: "Show one event and who has answered what",
			Args:    []ctlkit.Arg{{Name: "event-id", Required: true, ID: true, Help: "the id, as events prints it"}},
			Flags: []ctlkit.Flag{
				ctlkit.JSONFlag,
				ctlkit.FieldsFlag,
				{Name: "raw", Kind: ctlkit.Bool, Help: "write the answer from Spond to stdout as it came, with everything in it"},
			},
			Run: a.event,
		},
		{
			Name:    "attachment",
			Summary: "Download a file that is attached to an event",
			Args: []ctlkit.Arg{
				{Name: "event-id", Required: true, ID: true, Help: "the id, as events prints it"},
				{Name: "attachment-id", Required: true, ID: true, Help: "the id, as event prints it"},
			},
			Flags: []ctlkit.Flag{
				{Name: "out", Kind: ctlkit.String, Value: "path", Help: "the file to write, or a directory to write it in. Without the flag: the working directory"},
				{Name: "replace", Kind: ctlkit.Bool, Help: "overwrite a file that is already there"},
				ctlkit.JSONFlag,
			},
			Run: a.attachment,
		},
		{
			Name:    "respond",
			Summary: "Accept or decline an event on behalf of a member",
			Mutates: true,
			Args: []ctlkit.Arg{
				{Name: "event-id", Required: true, ID: true, Help: "the id, as events prints it"},
				{Name: "member-id", Required: true, ID: true, Help: "the member who answers, by the id members prints"},
				{Name: "answer", Required: true, Help: "accept or decline"},
			},
			Flags: []ctlkit.Flag{ctlkit.DryRunFlag, ctlkit.YesFlag, ctlkit.JSONFlag},
			Run:   a.respond,
		},
	}
}

func (a *App) login(ctx context.Context, c *ctlkit.Call) error {
	email := c.String("email")
	if email == "" {
		// Stdin can hold the password or the answer to a prompt, not both.
		if c.Bool("password-stdin") {
			return ctlkit.Usagef("--email is required when the password comes from stdin")
		}
		var err error
		if email, err = c.Ask("Email", "email"); err != nil {
			return err
		}
	}
	password, err := c.Secret("Password", "password-stdin")
	if err != nil {
		return err
	}

	token, err := a.Client.Login(ctx, email, password)
	if err != nil {
		if ctlkit.KindOf(err) == ctlkit.KindAuth {
			return ctlkit.Authf("%w", err).WithHint("check the email and the password. An account with two-factor login cannot log in this way.")
		}
		return err
	}
	if err := a.Tokens.Set(token); err != nil {
		return fmt.Errorf("store token in keyring: %w", err)
	}
	if a.Getenv(envToken) != "" {
		c.Note("%s is set and takes precedence over the stored token.", envToken)
	}
	fmt.Fprintln(c.Stdout, "Logged in. The token is in the system keyring.")
	return nil
}

func (a *App) logout(ctx context.Context, c *ctlkit.Call) error {
	if err := a.Tokens.Delete(); err != nil {
		return fmt.Errorf("remove token from keyring: %w", err)
	}
	if a.Getenv(envToken) != "" {
		c.Note("%s is still set and keeps working.", envToken)
	}
	fmt.Fprintln(c.Stdout, "Removed the stored token.")
	return nil
}

// groupSummary is one group in the list.
type groupSummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Members int    `json:"members"`
}

func (a *App) groups(ctx context.Context, c *ctlkit.Call) error {
	if err := a.authorize(); err != nil {
		return err
	}
	groups, err := a.Client.Groups(ctx)
	if err != nil {
		return err
	}
	list := make([]groupSummary, len(groups))
	for i, group := range groups {
		list[i] = groupSummary{ID: group.ID, Name: group.Name, Members: len(group.Members)}
	}
	return c.Print(list, func(w io.Writer) { renderGroups(w, list) })
}

// person is someone by id and name.
type person struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type memberView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Guardians []person `json:"guardians"`
}

func (a *App) members(ctx context.Context, c *ctlkit.Call) error {
	if err := a.authorize(); err != nil {
		return err
	}
	groups, err := a.Client.Groups(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(groups, func(g spond.Group) bool { return g.ID == c.Arg(0) })
	if i < 0 {
		return ctlkit.NotFoundf("the account is in no group with the id %q", c.Arg(0)).WithHint(`run "%s groups" to see the ids.`, Name)
	}

	group := groups[i]
	list := make([]memberView, len(group.Members))
	for i, member := range group.Members {
		view := memberView{ID: member.ID, Name: fullName(member.FirstName, member.LastName), Guardians: []person{}}
		for _, guardian := range member.Guardians {
			view.Guardians = append(view.Guardians, person{ID: guardian.ID, Name: fullName(guardian.FirstName, guardian.LastName)})
		}
		list[i] = view
	}
	return c.Print(list, func(w io.Writer) { renderMembers(w, group.Name, list) })
}

// responseCounts says how many of the invited gave each answer.
type responseCounts struct {
	Accepted    int `json:"accepted"`
	Declined    int `json:"declined"`
	Unanswered  int `json:"unanswered"`
	Waitinglist int `json:"waitinglist"`
	Unconfirmed int `json:"unconfirmed"`
}

func count(r spond.Responses) responseCounts {
	return responseCounts{
		Accepted:    len(r.AcceptedIDs),
		Declined:    len(r.DeclinedIDs),
		Unanswered:  len(r.UnansweredIDs),
		Waitinglist: len(r.WaitinglistIDs),
		Unconfirmed: len(r.UnconfirmedIDs),
	}
}

type place struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address,omitempty"`
}

type eventSummary struct {
	ID        string         `json:"id"`
	Heading   string         `json:"heading"`
	Start     string         `json:"start"`
	End       string         `json:"end"`
	Meetup    string         `json:"meetup,omitempty"`
	Cancelled bool           `json:"cancelled"`
	Location  *place         `json:"location,omitempty"`
	Responses responseCounts `json:"responses"`
	// Attachments is how many files the event has. event lists them.
	Attachments int `json:"attachments"`
}

// eventList is what events prints. Truncated says that Spond has more
// events in the period than was asked for.
type eventList struct {
	Items     []eventSummary `json:"items"`
	Truncated bool           `json:"truncated"`
}

func placeOf(l *spond.Location) *place {
	if l == nil || (l.Feature == "" && l.Address == "") {
		return nil
	}
	return &place{Name: l.Feature, Address: l.Address}
}

func summarize(e spond.Event) eventSummary {
	return eventSummary{
		ID:          e.ID,
		Heading:     e.Heading,
		Start:       e.StartTimestamp,
		End:         e.EndTimestamp,
		Meetup:      e.MeetupTimestamp,
		Cancelled:   e.Cancelled,
		Location:    placeOf(e.Location),
		Responses:   count(e.Responses),
		Attachments: len(e.Attachments),
	}
}

// date reads a yyyy-mm-dd flag as the start of that day in the user's zone.
func (a *App) date(c *ctlkit.Call, flag string) (time.Time, error) {
	day, err := time.ParseInLocation(dateLayout, c.String(flag), a.Zone)
	if err != nil {
		return time.Time{}, ctlkit.Usagef("--%s takes a date as yyyy-mm-dd, not %q", flag, c.String(flag))
	}
	return day, nil
}

func (a *App) events(ctx context.Context, c *ctlkit.Call) error {
	limit := c.Int("limit")
	if limit < 1 || limit >= spond.MaxEvents {
		return ctlkit.Usagef("--limit takes 1 to %d, not %d", spond.MaxEvents-1, limit)
	}
	query := spond.EventQuery{GroupID: c.String("group"), Max: limit + 1}
	if c.Given("group") {
		if err := ctlkit.CheckID("group id", query.GroupID); err != nil {
			return err
		}
	}

	now := a.Now().In(a.Zone)
	query.EndsFrom = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, a.Zone)
	var err error
	if c.Given("from") {
		if query.EndsFrom, err = a.date(c, "from"); err != nil {
			return err
		}
	}
	if c.Given("to") {
		to, err := a.date(c, "to")
		if err != nil {
			return err
		}
		if to.Before(query.EndsFrom) {
			return ctlkit.Usagef("--to %s is before --from %s", to.Format(dateLayout), query.EndsFrom.Format(dateLayout))
		}
		// The whole day counts, so the limit is where the next day starts.
		query.StartsBefore = to.AddDate(0, 0, 1)
	}

	if err := a.authorize(); err != nil {
		return err
	}
	// One more than asked for tells whether the list was cut off.
	events, err := a.Client.Events(ctx, query)
	if err != nil {
		return err
	}
	slices.SortStableFunc(events, func(x, y spond.Event) int { return strings.Compare(x.StartTimestamp, y.StartTimestamp) })

	list := eventList{Items: make([]eventSummary, 0, len(events))}
	for _, event := range events {
		list.Items = append(list.Items, summarize(event))
	}
	if len(list.Items) > limit {
		list.Items, list.Truncated = list.Items[:limit], true
	}
	return c.Print(list, func(w io.Writer) { a.renderEvents(w, list) })
}

// answered names the invited, by what they answered.
type answered struct {
	Accepted    []person `json:"accepted"`
	Declined    []person `json:"declined"`
	Unanswered  []person `json:"unanswered"`
	Waitinglist []person `json:"waitinglist"`
	Unconfirmed []person `json:"unconfirmed"`
}

type eventView struct {
	ID          string           `json:"id"`
	Heading     string           `json:"heading"`
	Description string           `json:"description,omitempty"`
	Start       string           `json:"start"`
	End         string           `json:"end"`
	Meetup      string           `json:"meetup,omitempty"`
	Cancelled   bool             `json:"cancelled"`
	Location    *place           `json:"location,omitempty"`
	Attachments []attachmentView `json:"attachments"`
	Responses   answered         `json:"responses"`
}

type attachmentView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Added string `json:"added,omitempty"`
}

// names maps the id of every member in the account's groups to a name.
func names(groups []spond.Group) map[string]string {
	out := make(map[string]string)
	for _, group := range groups {
		for _, member := range group.Members {
			out[member.ID] = fullName(member.FirstName, member.LastName)
		}
	}
	return out
}

func people(ids []string, names map[string]string) []person {
	out := make([]person, len(ids))
	for i, id := range ids {
		out[i] = person{ID: id, Name: names[id]}
	}
	return out
}

func (a *App) event(ctx context.Context, c *ctlkit.Call) error {
	if err := a.authorize(); err != nil {
		return err
	}
	if c.Bool("raw") {
		raw, err := a.Client.EventRaw(ctx, c.Arg(0))
		if err != nil {
			return withEventHint(err)
		}
		_, err = fmt.Fprintf(c.Stdout, "%s\n", raw)
		return err
	}
	event, err := a.Client.Event(ctx, c.Arg(0))
	if err != nil {
		return withEventHint(err)
	}
	// An event holds only the ids of the invited. The groups hold the names.
	groups, err := a.Client.Groups(ctx)
	if err != nil {
		return err
	}
	who := names(groups)

	view := eventView{
		ID:          event.ID,
		Heading:     event.Heading,
		Description: event.Description,
		Start:       event.StartTimestamp,
		End:         event.EndTimestamp,
		Meetup:      event.MeetupTimestamp,
		Cancelled:   event.Cancelled,
		Location:    placeOf(event.Location),
		Attachments: []attachmentView{},
		Responses: answered{
			Accepted:    people(event.Responses.AcceptedIDs, who),
			Declined:    people(event.Responses.DeclinedIDs, who),
			Unanswered:  people(event.Responses.UnansweredIDs, who),
			Waitinglist: people(event.Responses.WaitinglistIDs, who),
			Unconfirmed: people(event.Responses.UnconfirmedIDs, who),
		},
	}
	for _, file := range event.Attachments {
		view.Attachments = append(view.Attachments, attachmentView{ID: file.ID, Title: file.Title, URL: file.Media, Added: file.Timestamp})
	}
	return c.Print(view, func(w io.Writer) { a.renderEvent(w, view) })
}

// attachmentResult is what attachment prints.
type attachmentResult struct {
	EventID string `json:"eventId"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Path    string `json:"path"`
	Bytes   int    `json:"bytes"`
}

func (a *App) attachment(ctx context.Context, c *ctlkit.Call) error {
	if err := a.authorize(); err != nil {
		return err
	}
	event, err := a.Client.Event(ctx, c.Arg(0))
	if err != nil {
		return withEventHint(err)
	}
	i := slices.IndexFunc(event.Attachments, func(f spond.Attachment) bool { return f.ID == c.Arg(1) })
	if i < 0 {
		return ctlkit.NotFoundf("the event %q has no attachment with the id %q", oneLine(event.Heading), c.Arg(1)).
			WithHint(`run "%s event %s" to see the %d it has.`, Name, event.ID, len(event.Attachments))
	}
	file := event.Attachments[i]

	path, err := attachmentPath(c.String("out"), file.Media)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil && !c.Bool("replace") {
		return ctlkit.Errorf(ctlkit.KindConflict, "%s is already there", path).WithHint("pass --replace to overwrite it, or --out to write somewhere else.")
	}

	data, err := a.Client.Download(ctx, file.Media)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write the attachment: %w", err)
	}
	result := attachmentResult{EventID: event.ID, ID: file.ID, Title: file.Title, Path: path, Bytes: len(data)}
	return c.Print(result, func(w io.Writer) {
		fmt.Fprintf(w, "Saved %q to %s (%d bytes).\n", oneLine(file.Title), path, len(data))
	})
}

// attachmentPath decides where a download goes. The name comes from the
// address of the file, which the service chose, so only its last element is
// used and it can never point out of the directory.
func attachmentPath(out, media string) (string, error) {
	name := "attachment"
	if u, err := url.Parse(media); err == nil {
		if base := filepath.Base(filepath.FromSlash(u.Path)); base != "." && base != string(filepath.Separator) && base != ".." {
			name = base
		}
	}
	if out == "" {
		return name, nil
	}
	if info, err := os.Stat(out); err == nil && info.IsDir() {
		return filepath.Join(out, name), nil
	}
	return out, nil
}

// respondResult is what respond prints. With DryRun set, nothing was changed
// and Request is what would have been sent.
type respondResult struct {
	DryRun  bool                  `json:"dryRun"`
	Event   eventRef              `json:"event"`
	Member  person                `json:"member"`
	Answer  string                `json:"answer"`
	Request spond.ResponseRequest `json:"request"`
	// Responses is how the invited have answered after the change.
	Responses *responseCounts `json:"responses,omitempty"`
}

type eventRef struct {
	ID      string `json:"id"`
	Heading string `json:"heading"`
	Start   string `json:"start"`
}

func (a *App) respond(ctx context.Context, c *ctlkit.Call) error {
	answer := strings.ToLower(c.Arg(2))
	if !slices.Contains(answers, answer) {
		return ctlkit.Usagef("the answer %q is not one of: %s", c.Arg(2), strings.Join(answers, ", "))
	}
	accepted := "false"
	if answer == "accept" {
		accepted = "true"
	}
	if err := a.authorize(); err != nil {
		return err
	}

	// Both are read first, also in a dry run, so that the question names the
	// event and the member instead of two ids nobody can check.
	event, err := a.Client.Event(ctx, c.Arg(0))
	if err != nil {
		return withEventHint(err)
	}
	groups, err := a.Client.Groups(ctx)
	if err != nil {
		return err
	}
	name, ok := names(groups)[c.Arg(1)]
	if !ok {
		return ctlkit.NotFoundf("no group of the account has a member with the id %q", c.Arg(1)).
			WithHint(`run "%s members <group-id>" to see the ids.`, Name)
	}
	r := event.Responses
	if !slices.Contains(slices.Concat(r.AcceptedIDs, r.DeclinedIDs, r.UnansweredIDs, r.WaitinglistIDs, r.UnconfirmedIDs), c.Arg(1)) {
		c.Note("%s is not among the invited that the event lists.", name)
	}

	result := respondResult{
		Event:   eventRef{ID: event.ID, Heading: event.Heading, Start: event.StartTimestamp},
		Member:  person{ID: c.Arg(1), Name: name},
		Answer:  answer,
		Request: spond.ResponseRequest{Accepted: accepted},
	}
	if c.DryRun() {
		result.DryRun = true
		return c.Print(result, func(w io.Writer) { a.renderRespond(w, result) })
	}

	question := fmt.Sprintf("%s %q at %s for %s?", strings.ToUpper(answer[:1])+answer[1:], oneLine(event.Heading), a.local(event.StartTimestamp), name)
	if err := c.Confirm(question); err != nil {
		return err
	}
	responses, err := a.Client.Respond(ctx, event.ID, c.Arg(1), result.Request)
	if err != nil {
		return err
	}
	counts := count(*responses)
	result.Responses = &counts
	return c.Print(result, func(w io.Writer) { a.renderRespond(w, result) })
}

// withEventHint tells where the ids come from when Spond does not know one.
func withEventHint(err error) error {
	if ctlkit.KindOf(err) != ctlkit.KindNotFound {
		return err
	}
	return ctlkit.NotFoundf("%w", err).WithHint(`run "%s events" to see the ids.`, Name)
}

func fullName(first, last string) string {
	return strings.TrimSpace(first + " " + last)
}
