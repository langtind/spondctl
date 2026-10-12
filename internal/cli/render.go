package cli

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Output is Markdown, which reads well both in a terminal and when an agent
// relays it into a chat. What Spond returned is printed on one line, so that
// a line break in a name cannot pass for structure.

func renderGroups(w io.Writer, groups []groupSummary) {
	fmt.Fprintln(w, "# Groups")
	if len(groups) == 0 {
		fmt.Fprintln(w, "\nThe account is in no groups.")
		return
	}
	fmt.Fprintln(w)
	for _, group := range groups {
		fmt.Fprintf(w, "- %s\n", oneLine(group.Name))
		field(w, "Members", fmt.Sprint(group.Members))
		field(w, "id", group.ID)
	}
}

func renderMembers(w io.Writer, group string, members []memberView) {
	fmt.Fprintf(w, "# Members of %s\n", oneLine(group))
	if len(members) == 0 {
		fmt.Fprintln(w, "\nThe group has no members.")
		return
	}
	fmt.Fprintln(w)
	for _, member := range members {
		fmt.Fprintf(w, "- %s\n", orDash(member.Name))
		field(w, "Guardians", personNames(member.Guardians))
		field(w, "id", member.ID)
	}
}

func (a *App) renderEvents(w io.Writer, list eventList) {
	fmt.Fprintln(w, "# Events")
	if len(list.Items) == 0 {
		fmt.Fprintln(w, "\nNo events in the period.")
		return
	}
	fmt.Fprintln(w)
	for _, event := range list.Items {
		fmt.Fprintf(w, "- %s\n", heading(event.Heading, event.Cancelled))
		field(w, "Start", a.local(event.Start))
		field(w, "End", a.local(event.End))
		field(w, "Meet", a.local(event.Meetup))
		field(w, "Place", placeLine(event.Location))
		r := event.Responses
		field(w, "Answers", fmt.Sprintf("%d accepted, %d declined, %d unanswered", r.Accepted, r.Declined, r.Unanswered)+
			extra(r.Waitinglist, "on the waiting list")+extra(r.Unconfirmed, "unconfirmed"))
		if event.Attachments > 0 {
			field(w, "Attachments", fmt.Sprint(event.Attachments))
		}
		field(w, "id", event.ID)
	}
	if list.Truncated {
		fmt.Fprintln(w, "\nThere are more. Raise --limit or narrow the dates to see them.")
	}
}

func (a *App) renderEvent(w io.Writer, event eventView) {
	fmt.Fprintf(w, "# %s\n\n", heading(event.Heading, event.Cancelled))
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(w, "%s: %s\n", label, oneLine(value))
		}
	}
	line("Start", a.local(event.Start))
	line("End", a.local(event.End))
	line("Meet", a.local(event.Meetup))
	line("Place", placeLine(event.Location))
	line("id", event.ID)

	if text := strings.TrimSpace(event.Description); text != "" {
		fmt.Fprintf(w, "\n## Description\n\n%s\n", text)
	}

	if len(event.Attachments) > 0 {
		fmt.Fprintln(w, "\n## Attachments")
		fmt.Fprintln(w)
		for _, file := range event.Attachments {
			fmt.Fprintf(w, "- %s (id %s)\n", orDash(file.Title), file.ID)
		}
	}

	fmt.Fprintln(w, "\n## Answers")
	group := func(label string, who []person) {
		if len(who) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s (%d):\n", label, len(who))
		for _, p := range who {
			fmt.Fprintf(w, "- %s (id %s)\n", orDash(p.Name), p.ID)
		}
	}
	r := event.Responses
	group("Accepted", r.Accepted)
	group("Declined", r.Declined)
	group("Unanswered", r.Unanswered)
	group("On the waiting list", r.Waitinglist)
	group("Unconfirmed", r.Unconfirmed)
	if len(r.Accepted)+len(r.Declined)+len(r.Unanswered)+len(r.Waitinglist)+len(r.Unconfirmed) == 0 {
		fmt.Fprintln(w, "\nNobody is invited.")
	}
}

func (a *App) renderRespond(w io.Writer, result respondResult) {
	what := fmt.Sprintf("%s %q at %s for %s", result.Answer, oneLine(result.Event.Heading), a.local(result.Event.Start), result.Member.Name)
	if result.DryRun {
		fmt.Fprintf(w, "Dry run. Would %s. Nothing was changed.\n", what)
		return
	}
	fmt.Fprintf(w, "Done: %s.\n", what)
	if r := result.Responses; r != nil {
		fmt.Fprintf(w, "Answers now: %d accepted, %d declined, %d unanswered.\n", r.Accepted, r.Declined, r.Unanswered)
	}
}

// local shows a time from Spond in the user's zone, still as RFC 3339. A
// value that is not a time is shown as it is.
func (a *App) local(stamp string) string {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return stamp
	}
	return t.In(a.Zone).Format(time.RFC3339)
}

func heading(text string, cancelled bool) string {
	text = orDash(text)
	if cancelled {
		text += " (cancelled)"
	}
	return text
}

func placeLine(p *place) string {
	if p == nil {
		return ""
	}
	if p.Name != "" && p.Address != "" {
		return p.Name + ", " + p.Address
	}
	return p.Name + p.Address
}

func personNames(people []person) string {
	names := make([]string, len(people))
	for i, p := range people {
		names[i] = orDash(p.Name)
	}
	return strings.Join(names, ", ")
}

func extra(n int, what string) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(", %d %s", n, what)
}

// field writes one indented line under a list item, and nothing for an
// empty value.
func field(w io.Writer, label, value string) {
	if value != "" {
		fmt.Fprintf(w, "  %s: %s\n", label, oneLine(value))
	}
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func orDash(s string) string {
	if s = oneLine(s); s == "" {
		return "-"
	}
	return s
}
