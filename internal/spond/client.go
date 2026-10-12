// Package spond is the client for the Spond API, the one the Spond apps use.
// reference/README.md lists what is verified and what is only reported.
package spond

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/langtind/ctlkit"
	"github.com/langtind/ctlkit/httpx"
)

const (
	service = "Spond"

	// MaxEvents is the most events one request asks for.
	MaxEvents = 100

	// timestamp is how the API takes a point in time in a query.
	timestamp = "2006-01-02T15:04:05.000Z"
)

type Client struct {
	HTTP      *http.Client
	BaseURL   string
	UserAgent string
	// Token is sent as a bearer token when it is set.
	Token string
}

func NewClient() *Client {
	return &Client{HTTP: httpx.NewClient(), BaseURL: "https://api.spond.com/core/v1"}
}

// Group is a group the account belongs to, with its members.
type Group struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []Member `json:"members"`
}

// Member is one member of a group. A child has guardians.
type Member struct {
	ID        string   `json:"id"`
	FirstName string   `json:"firstName"`
	LastName  string   `json:"lastName"`
	Guardians []Person `json:"guardians"`
}

type Person struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

// Event is what Spond calls a spond: something members are invited to.
type Event struct {
	ID              string    `json:"id"`
	Heading         string    `json:"heading"`
	Description     string    `json:"description"`
	StartTimestamp  string    `json:"startTimestamp"`
	EndTimestamp    string    `json:"endTimestamp"`
	MeetupTimestamp string    `json:"meetupTimestamp"`
	Cancelled       bool      `json:"cancelled"`
	Location        *Location `json:"location"`
	Responses       Responses `json:"responses"`
}

type Location struct {
	// Feature is the name of the place.
	Feature string `json:"feature"`
	Address string `json:"address"`
}

// Responses holds the member ids of the invited, by what they answered.
type Responses struct {
	AcceptedIDs    []string `json:"acceptedIds"`
	DeclinedIDs    []string `json:"declinedIds"`
	UnansweredIDs  []string `json:"unansweredIds"`
	WaitinglistIDs []string `json:"waitinglistIds"`
	UnconfirmedIDs []string `json:"unconfirmedIds"`
}

// Login trades an email and a password for an access token. An account with
// two-factor login gets no token this way.
func (c *Client) Login(ctx context.Context, email, password string) (string, error) {
	var answer struct {
		AccessToken struct {
			Token string `json:"token"`
		} `json:"accessToken"`
	}
	body := map[string]string{"email": email, "password": password}
	if err := c.do(ctx, http.MethodPost, "/auth2/login", nil, body, &answer); err != nil {
		return "", err
	}
	if answer.AccessToken.Token == "" {
		return "", ctlkit.Authf("%s gave no access token", service).
			WithHint("check the email and the password. An account with two-factor login cannot log in this way.")
	}
	return answer.AccessToken.Token, nil
}

// Groups lists the groups the account belongs to, with their members.
func (c *Client) Groups(ctx context.Context) ([]Group, error) {
	groups := []Group{}
	err := c.do(ctx, http.MethodGet, "/groups/", nil, nil, &groups)
	return groups, err
}

// EventQuery narrows an event search. The zero value of a field leaves it out.
type EventQuery struct {
	GroupID string
	// EndsFrom keeps the events that end at this time or later.
	EndsFrom time.Time
	// StartsBefore keeps the events that start at this time or earlier.
	StartsBefore time.Time
	Max          int
}

// Events lists events. The order is the one the service chose.
func (c *Client) Events(ctx context.Context, q EventQuery) ([]Event, error) {
	query := url.Values{"max": {strconv.Itoa(q.Max)}, "scheduled": {"false"}}
	if q.GroupID != "" {
		query.Set("groupId", q.GroupID)
	}
	if !q.EndsFrom.IsZero() {
		query.Set("minEndTimestamp", q.EndsFrom.UTC().Format(timestamp))
	}
	if !q.StartsBefore.IsZero() {
		query.Set("maxStartTimestamp", q.StartsBefore.UTC().Format(timestamp))
	}
	events := []Event{}
	err := c.do(ctx, http.MethodGet, "/sponds/", query, nil, &events)
	return events, err
}

func (c *Client) Event(ctx context.Context, id string) (*Event, error) {
	var event Event
	query := url.Values{"includeComments": {"true"}}
	err := c.do(ctx, http.MethodGet, "/sponds/"+url.PathEscape(id), query, nil, &event)
	return &event, err
}

// ResponseRequest is the body Respond sends. A dry run prints it. The API
// takes the answer as text, "true" or "false".
type ResponseRequest struct {
	Accepted string `json:"accepted"`
}

// Respond answers an event on behalf of a member, and returns the answers
// of everyone invited as they are afterwards. Answering the same again
// changes nothing.
func (c *Client) Respond(ctx context.Context, eventID, memberID string, request ResponseRequest) (*Responses, error) {
	var responses Responses
	path := "/sponds/" + url.PathEscape(eventID) + "/responses/" + url.PathEscape(memberID)
	err := c.do(ctx, http.MethodPut, path, nil, request, &responses)
	return &responses, err
}

// do sends one request and decodes a 2xx answer into into. Any other status
// comes back as an error with a kind, see httpx.Do.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, into any) error {
	target := c.BaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	payload := bytes.NewReader(nil)
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	answer, err := httpx.Do(c.HTTP, req, service, c.UserAgent)
	if err != nil {
		return err
	}
	if into == nil || len(answer) == 0 {
		return nil
	}
	if err := json.Unmarshal(answer, into); err != nil {
		return fmt.Errorf("%s: read the answer to %s %s: %w", service, method, path, err)
	}
	return nil
}
