// Package cli implements the spondctl command line.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/langtind/ctlkit"
	"github.com/langtind/ctlkit/keystore"
	"github.com/langtind/ctlkit/session"

	"github.com/langtind/spondctl/internal/spond"
)

const (
	// Name is the program name. A rename also touches the module path, the
	// skill directory with its embed line in skill_embed.go, and the docs.
	Name = "spondctl"

	summary = "read groups and events on Spond, and answer invitations"

	// envToken names the variable that overrides the keyring. Cron jobs and
	// containers have no keyring to unlock. A token from the environment is
	// used as it is and never renewed.
	envToken = "SPONDCTL_TOKEN"
)

// App holds what the commands work with. Tests replace the fields to point
// spondctl at a fake service and at a keyring in memory.
type App struct {
	Client *spond.Client
	// Session holds the login and renews it.
	Session *session.Manager
	Getenv  func(string) string
	// Now is the clock, and Zone the time zone that dates are read in and
	// times are shown in.
	Now  func() time.Time
	Zone *time.Location
}

// New returns spondctl as it runs for real.
func New(build ctlkit.Build, skill fs.FS) *ctlkit.Program {
	app := &App{
		Client:  spond.NewClient(),
		Session: &session.Manager{Store: keystore.Keyring(Name, "token"), LockPath: session.LockFile(Name)},
		Getenv:  os.Getenv,
		Now:     time.Now,
		Zone:    time.Local,
	}
	return app.program(build, skill)
}

func (a *App) program(build ctlkit.Build, skill fs.FS) *ctlkit.Program {
	p := ctlkit.New(Name, summary, build)
	p.URL = "https://github.com/langtind/" + Name
	p.Skill = skill
	p.Commands = a.commands()
	p.Hint = hint
	a.Client.UserAgent = p.UserAgent()
	a.Session.Renew = a.Client.RenewSession
	a.Session.Now = a.Now
	return p
}

// hint says what to do about an error that cannot say it itself.
func hint(err error) string {
	if ctlkit.KindOf(err) == ctlkit.KindAuth {
		return fmt.Sprintf(`run "%s login" again.`, Name)
	}
	return ""
}

// loginError adds what to do to an error from the session.
func loginError(err error) error {
	switch {
	case errors.Is(err, session.ErrNotLoggedIn):
		return ctlkit.Authf("%w", err).WithHint(`run "%s login", or set %s.`, Name, envToken)
	case errors.Is(err, session.ErrExpired):
		return ctlkit.Authf("%w", err).WithHint(`run "%s login" again.`, Name)
	}
	return err
}

// authorize gives the client a token: the one from the environment, or else
// the stored login, renewed when it is about to run out. A stored login is
// also renewed when Spond refuses its token in the middle of a run.
func (a *App) authorize(ctx context.Context) error {
	if token := strings.TrimSpace(a.Getenv(envToken)); token != "" {
		a.Client.Token = token
		return nil
	}
	token, err := a.Session.Token(ctx)
	if err != nil {
		return loginError(err)
	}
	a.Client.Token = token
	a.Client.Renew = func(ctx context.Context) (string, error) {
		token, err := a.Session.Refresh(ctx, a.Client.Token)
		return token, loginError(err)
	}
	return nil
}
