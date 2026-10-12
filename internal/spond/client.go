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
	"strings"
	"time"

	"github.com/langtind/ctlkit"
	"github.com/langtind/ctlkit/httpx"
	"github.com/langtind/ctlkit/session"
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
	// Renew returns a new token after the service refused Token with a 401.
	// The request is then sent once more. Nil means a 401 is final.
	Renew func(ctx context.Context) (string, error)
	// MediaOrigin is where attachments are stored. Download fetches nothing
	// from anywhere else.
	MediaOrigin string
}

func NewClient() *Client {
	return &Client{HTTP: httpx.NewClient(), BaseURL: "https://api.spond.com/core/v1", MediaOrigin: "https://spond.com"}
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
	ID              string       `json:"id"`
	Heading         string       `json:"heading"`
	Description     string       `json:"description"`
	StartTimestamp  string       `json:"startTimestamp"`
	EndTimestamp    string       `json:"endTimestamp"`
	MeetupTimestamp string       `json:"meetupTimestamp"`
	Cancelled       bool         `json:"cancelled"`
	Location        *Location    `json:"location"`
	Responses       Responses    `json:"responses"`
	Attachments     []Attachment `json:"attachments"`
}

// Attachment is a file on an event.
type Attachment struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Media is the address of the file.
	Media     string `json:"media"`
	Timestamp string `json:"timestamp"`
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

// tokens is what a login and a renewal answer: an access token that lasts
// a day, and a refresh token that lasts about three months.
type tokens struct {
	AccessToken  timedToken `json:"accessToken"`
	RefreshToken timedToken `json:"refreshToken"`
}

type timedToken struct {
	Token      string    `json:"token"`
	Expiration time.Time `json:"expiration"`
}

func (t tokens) session() session.Session {
	return session.Session{
		AccessToken:    t.AccessToken.Token,
		AccessExpires:  t.AccessToken.Expiration,
		RefreshToken:   t.RefreshToken.Token,
		RefreshExpires: t.RefreshToken.Expiration,
	}
}

// Login trades an email and a password for a session. An account with
// two-factor login gets no token this way.
func (c *Client) Login(ctx context.Context, email, password string) (session.Session, error) {
	var answer tokens
	body := map[string]string{"email": email, "password": password}
	if err := c.send(ctx, http.MethodPost, "/auth2/login", nil, body, &answer, false); err != nil {
		return session.Session{}, err
	}
	if answer.AccessToken.Token == "" {
		return session.Session{}, ctlkit.Authf("%s gave no access token", service).
			WithHint("check the email and the password. An account with two-factor login cannot log in this way.")
	}
	return answer.session(), nil
}

// RenewSession trades a refresh token for a new session. Spond hands out a
// new refresh token every time, and the one that was sent stops working.
func (c *Client) RenewSession(ctx context.Context, refreshToken string) (session.Session, error) {
	var answer tokens
	body := map[string]string{"value": refreshToken}
	if err := c.send(ctx, http.MethodPost, "/auth2/login/refresh", nil, body, &answer, false); err != nil {
		return session.Session{}, err
	}
	return answer.session(), nil
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

// Events lists events, the earliest first. Without "order" the service
// answers the latest first, and Max would then cut off the nearest ones.
func (c *Client) Events(ctx context.Context, q EventQuery) ([]Event, error) {
	query := url.Values{"max": {strconv.Itoa(q.Max)}, "scheduled": {"false"}, "order": {"asc"}}
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

// EventRaw returns the answer to Event as the service sent it.
func (c *Client) EventRaw(ctx context.Context, id string) ([]byte, error) {
	var raw json.RawMessage
	query := url.Values{"includeComments": {"true"}}
	err := c.do(ctx, http.MethodGet, "/sponds/"+url.PathEscape(id), query, nil, &raw)
	return raw, err
}

// Download fetches the file of an attachment. The address comes from an
// event, which is data from the service, so it is fetched only when it lies
// under MediaOrigin, and the token is never sent with it.
func (c *Client) Download(ctx context.Context, media string) ([]byte, error) {
	if !strings.HasPrefix(media, c.MediaOrigin+"/") {
		return nil, fmt.Errorf("%s: the attachment is stored at %q, which is not under %s", service, media, c.MediaOrigin)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, media, nil)
	if err != nil {
		return nil, err
	}
	return httpx.Do(c.HTTP, req, service, c.UserAgent)
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

// do sends one request with the token and decodes a 2xx answer into into.
// Any other status comes back as an error with a kind, see httpx.Do. When
// the service refuses the token, the session is renewed and the request is
// sent once more.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, into any) error {
	err := c.send(ctx, method, path, query, body, into, true)
	if httpx.Status(err) != http.StatusUnauthorized || c.Renew == nil {
		return err
	}
	token, renewErr := c.Renew(ctx)
	if renewErr != nil {
		return renewErr
	}
	c.Token = token
	return c.send(ctx, method, path, query, body, into, true)
}

func (c *Client) send(ctx context.Context, method, path string, query url.Values, body, into any, authorized bool) error {
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
	if authorized && c.Token != "" {
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
