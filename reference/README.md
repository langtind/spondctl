# Reference

The Spond API as far as it is known. Spond publishes no API. The client in `internal/spond` talks to the one the Spond apps use.

## Where this comes from

The requests below were read out of [Olen/Spond](https://github.com/Olen/Spond), an open Python client, at commit `51bca88` (2026-10-06). Only facts about the API were taken: paths, parameters and field names. That project is GPL-3.0 and none of its code is copied here.

No request has been captured from the app. The read requests have been run against a real account, see "Verified". The write request has not.

## Verified

Checked on 2026-10-12 with spondctl itself, without an account:

| Request | Answer |
| --- | --- |
| `POST /core/v1/auth2/login` with an email that does not exist | HTTP 401, `{"message": "Wrong email or password."}` |
| `GET /core/v1/groups/` with a bearer token that is not one | HTTP 401, `{"message": "Not authenticated"}` |

So the base URL, the login path and the bearer scheme are right, and an error carries its text in `message`.

Checked on 2026-10-12 with a real account. Only the shape is written down:

| Request | What the answer showed |
| --- | --- |
| `POST /auth2/login` | A token in `accessToken.token`. It is long, about 700 characters |
| `GET /groups/` | A list of groups with `id`, `name` and `members`, as reported |
| `GET /sponds/` with `groupId`, `minEndTimestamp` and `maxStartTimestamp` at a time of day that is not midnight | Only events in the group and in the period, each with `heading`, `startTimestamp`, `endTimestamp`, `location.feature`, `location.address` and the id lists. Timestamps are RFC 3339 in UTC |
| `GET /sponds/{id}?includeComments=true` | The fields below |
| `GET` of an attachment's `media` address, without a token | HTTP 200, `application/pdf` |

An event answers with these fields: `attachments`, `autoAccept`, `autoReminderType`, `behalfOfIds`, `comments`, `commentsDisabled`, `createdTime`, `creatorId`, `description`, `endTimestamp`, `expired`, `heading`, `hidden`, `id`, `location`, `matchEvent`, `owners`, `participantsHidden`, `picture`, `recipients`, `registered`, `responses`, `startTimestamp`, `tasks`, `type`, `updated`, `visibility`.

- `responses` has the five id lists and also `participantIds`.
- `location` has `id`, `feature`, `address`, `postalCode`, `country`, `latitude`, `longitude`, `administrativeAreaLevel1` and `administrativeAreaLevel2`.
- `recipients` has `group`, `guardians` and `profiles`.
- An attachment has `id`, `title`, `media`, `type` (2 for a PDF), `ownerId` and `timestamp`. `media` is an address under `https://spond.com/storage/upload/`, and the last element of its path is a file name.

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

- The order of `GET /sponds/`. spondctl sorts by start time itself.
- That `max` has no lower ceiling than 100.
- That every invited member is in one of the five id lists. `respond` only notes it when a member is in none.
- How long a token lasts. There is no known way to renew one.
- That the `PUT` answers the five id lists directly and not wrapped in something.

## To verify next

`respond` has never been run for real. Run it only with the consent of the member it concerns, and write down what the `PUT` answers.

Do not paste an answer here: it holds the names of real people, most of them children. Write down the shape only. `spondctl event <id> --raw` prints an answer as it came.
