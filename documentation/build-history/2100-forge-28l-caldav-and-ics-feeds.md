## Forge 28l: CalDAV and ICS feeds

Built 2026-11-28 (`Calendar.md` › *Built in forge 28l*). The connector types `calendar.caldav` and `calendar.ics` implement `calendar.events@1`. The `calendar/providers` package holds what they share, and `caldav/caldavtest` is an in-memory CalDAV server written from the RFCs. `ical.ComposeOptions.Resource` composes without `METHOD`. `calCalendar.readOnly` marks a calendar listed as `reader` or `freeBusy`, which `calendar-push` skips.

What later parts must do:
- A type reads every `.ics` a service returns through `providers.Reader`, which is the child in a server.
- A type's settings carry its polling, because adapters never read configuration.
- A connection with a `secret` setting sends it under `secrets`, and a test fixture that makes one needs a `config.SecretStore` in its connections' deps.
- Google and Microsoft (28j, 28k) reuse `calendar/providers` and pass the same suite.
