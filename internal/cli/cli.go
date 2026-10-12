// Package cli implements the spondctl command line.
package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/langtind/ctlkit"
	"github.com/langtind/ctlkit/keystore"

	"github.com/langtind/spondctl/internal/spond"
)

const (
	// Name is the program name. A rename also touches the module path, the
	// skill directory with its embed line in skill_embed.go, and the docs.
	Name = "spondctl"

	summary = "read groups and events on Spond, and answer invitations"

	// envToken names the variable that overrides the keyring. Cron jobs and
	// containers have no keyring to unlock.
	envToken = "SPONDCTL_TOKEN"
)

// App holds what the commands work with. Tests replace the fields to point
// spondctl at a fake service and at a keyring in memory.
type App struct {
	Client *spond.Client
	Tokens keystore.Store
	Getenv func(string) string
	// Now is the clock, and Zone the time zone that dates are read in and
	// times are shown in.
	Now  func() time.Time
	Zone *time.Location
}

// New returns spondctl as it runs for real.
func New(build ctlkit.Build, skill fs.FS) *ctlkit.Program {
	app := &App{
		Client: spond.NewClient(),
		Tokens: keystore.Keyring(Name, "token"),
		Getenv: os.Getenv,
		Now:    time.Now,
		Zone:   time.Local,
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
	return p
}

var errNotLoggedIn = ctlkit.Authf("not logged in").WithHint(`run "%s login", or set %s.`, Name, envToken)

// hint says what to do about an error that cannot say it itself. Spond
// answers 401 when a token has run out, and there is no way to renew one.
func hint(err error) string {
	if ctlkit.KindOf(err) == ctlkit.KindAuth {
		return fmt.Sprintf(`the token may have run out. Run "%s login" again.`, Name)
	}
	return ""
}

// authorize gives the client the token from the environment, or else from
// the keyring.
func (a *App) authorize() error {
	if token := strings.TrimSpace(a.Getenv(envToken)); token != "" {
		a.Client.Token = token
		return nil
	}
	token, err := a.Tokens.Get()
	switch {
	case err == nil && token != "":
		a.Client.Token = token
		return nil
	case err == nil || errors.Is(err, keystore.ErrNotFound):
		return errNotLoggedIn
	}
	return fmt.Errorf("read token from keyring: %w", err)
}
