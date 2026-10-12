# Reference

The Spond API as far as it is known. Spond publishes no API. The client in `internal/spond` talks to the one the Spond apps use.

## Where this comes from

The requests below were read out of [Olen/Spond](https://github.com/Olen/Spond), an open Python client, at commit `51bca88` (2026-10-06). Only facts about the API were taken: paths, parameters and field names. That project is GPL-3.0 and none of its code is copied here.

No request has been captured from the app yet, and no answer to a logged-in request has been seen. Until one has, everything under "Reported by others" is a lead and not a fact.

## Verified

Checked on 2026-10-12 with spondctl itself, without an account:

| Request | Answer |
| --- | --- |
| `POST /core/v1/auth2/login` with an email that does not exist | HTTP 401, `{"message": "Wrong email or password."}` |
| `GET /core/v1/groups/` with a bearer token that is not one | HTTP 401, `{"message": "Not authenticated"}` |

So the base URL, the login path and the bearer scheme are right, and an error carries its text in `message`.

## Reported by others

Base URL `https://api.spond.com/core/v1`. Every request after login carries `Authorization: Bearer <token>`.

| What | Request | Notes |
| --- | --- | --- |
| Log in | `POST /auth2/login` with `{"email", "password"}` | Answers `{"accessToken": {"token", "expiration"}, ...}`. An account with two-factor login gets an answer without `accessToken`, which can hold a phone number and a challenge token. Never print that answer |
| Groups | `GET /groups/` | A list. Each group has `id`, `name` and `members`. A member has `id`, `firstName`, `lastName`, `profile.id`, and `guardians` with the same name fields |
| Events | `GET /sponds/` | Query: `max`, `scheduled` (`false` leaves out events whose invitations are not sent yet), `groupId`, `subGroupId`, `includeHidden`, and `minEndTimestamp`, `maxEndTimestamp`, `minStartTimestamp`, `maxStartTimestamp` as `2006-01-02T15:04:05.000Z` |
| One event | `GET /sponds/{id}?includeComments=true` | 404 when the id is unknown |
| Answer | `PUT /sponds/{id}/responses/{memberId}` with `{"accepted": "true"}` or `{"accepted": "false"}` | The value is text. Answers the event's `responses` object. `memberId` is the id of the member in the group, not of the logged-in profile |

An event has `id`, `heading`, `description`, `startTimestamp`, `endTimestamp`, `meetupTimestamp`, `cancelled`, `location` with `feature` (the name of the place) and `address`, and `responses` with the id lists `acceptedIds`, `declinedIds`, `unansweredIds`, `waitinglistIds` and `unconfirmedIds`.

Not used by spondctl, and listed so that nobody has to look again: `GET /profile`, `GET /posts/`, `GET /sponds/{id}/export` (attendance as xlsx), `POST /sponds/{id}` (update an event), and chat on a separate host that `POST /chat` names.

## Unverified

What the client assumes without having seen it:

- That the timestamps in an event are RFC 3339 in UTC. `render.go` shows a value it cannot parse as it is.
- That the query timestamps take a time of day. The Python client only ever sends midnight UTC.
- The order of `GET /sponds/`. spondctl sorts by start time itself.
- That `max` has no lower ceiling than 100.
- That every invited member is in one of the five id lists. `respond` only notes it when a member is in none.
- How long a token lasts. There is no known way to renew one.
- That the `PUT` answers the five id lists directly and not wrapped in something.

## To verify next

Log in with a real account, run each read command with `--json`, and move what the answers confirm up to "Verified". Do not paste an answer here: it holds the names of real people, most of them children. Write down the shape only.

Run `respond` for real only with the consent of the member it concerns.
