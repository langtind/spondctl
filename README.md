# spondctl

A command line tool for [Spond](https://spond.com), the app that sports teams and other groups use for events and attendance.

spondctl lists the groups you are in, shows upcoming events and who has answered them, and accepts or declines on behalf of a member. The output is Markdown, so it reads well in a terminal and when an AI agent relays it into a chat. Every data command also prints JSON.

> spondctl is unofficial and not affiliated with Spond. It uses the same undocumented API as the Spond apps, so it can break when they change.

## Install

```bash
brew install langtind/tap/spondctl
```

Or with Go:

```bash
go install github.com/langtind/spondctl@latest
```

## Use

```bash
spondctl login
spondctl events
spondctl event <event-id>
spondctl groups
spondctl members <group-id>
spondctl respond <event-id> <member-id> accept --dry-run
```

`spondctl login` asks for the email and the password and stores a token in the system keyring. The password is not stored. In a script, pipe the password in:

```bash
op read "op://vault/item/password" | spondctl login --email <email> --password-stdin
```

Set `SPONDCTL_TOKEN` to use a token without the keyring. A token runs out after a while, and then you log in again. An account with two-factor login cannot log in this way.

### Events

```bash
spondctl events                                    # what has not ended yet
spondctl events --from 2026-11-01 --to 2026-11-30
spondctl events --group <group-id> --limit 50
```

### Answer an invitation

```bash
spondctl respond <event-id> <member-id> decline --dry-run
spondctl respond <event-id> <member-id> decline --yes
```

`respond` reads the event and the member first, asks before it changes anything, and refuses when there is no terminal and no `--yes`. To answer for a child, use the child's member id from `spondctl members`.

## For agents

```bash
spondctl install-skill   # copies the skill to ~/.claude/skills
spondctl schema          # every command, flag and error kind as JSON
```

Add `--json` to a data command for one JSON document on stdout, and `--fields id,heading` to keep only some fields. A failed command exits with a code that says why: 2 wrong call, 3 not found, 4 not logged in, 6 rate limit, 7 service unavailable. With `--json` the error is a JSON document too.

## Develop

```bash
go build -o spondctl .
go test -race ./...
go vet ./...
```

## License

MIT
