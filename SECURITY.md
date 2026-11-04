# Security policy

DOINK is a World of Warcraft addon plus an optional companion program
(`doink.exe` on Windows, `doink` on Linux) that posts game events to a
Discord webhook. The README's "What doink.exe does (and doesn't)" section
describes exactly what the companion reads, writes and sends.

## Supported versions

Only the newest release on
[GitHub Releases](https://github.com/ryan-flan/DOINK/releases) receives
fixes. The companion offers one-click updates from its tray menu.

## Reporting a vulnerability

Please report privately through GitHub's
[Report a vulnerability](https://github.com/ryan-flan/DOINK/security/advisories/new)
form, not in a public issue. Include the version (`/doink` in game prints
the addon's; the tray menu title shows the companion's), what you found and
how to reproduce it. Expect an acknowledgement within a week. There is no
bug bounty.

Things that count: anything that makes the companion download or run code
it shouldn't (the update path), post something the player didn't do, leak a
webhook URL or other data beyond what the README lists, or that lets a
crafted SavedVariables file, screenshot or Discord reply break the
companion. A webhook URL in a `DOINK.lua` someone shared by hand is not a
vulnerability; the README warns about that.
