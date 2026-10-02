# DOINK

Discord notifications for **World of Warcraft: Forever**. Level-ups, rare
loot, deaths, quest turn-ins, boss kills and profession milestones get posted
to a Discord channel. Inspired by [Dink](https://github.com/pajlads/DinkPlugin)
for RuneLite. Status: beta, tracking the Forever beta client.

## How it works

WoW addons can't talk to the internet, so DOINK comes in two parts:

1. **The addon** notices events in game and queues them in its saved data.
2. **The companion** (`doink.exe`) runs alongside the game, reads that queue
   and posts each event to your Discord webhook.

> **Posts arrive when WoW saves addon data:** on `/reload`, logout, or exit.
> That's a WoW limitation, not a setting. Play normally and your session posts
> when you log out, or type `/doink flush` to post right away. There is also
> an opt-in, experimental **realtime** mode that posts while you play; see
> [How realtime works](#how-realtime-works-experimental).

## Install

1. Download `DOINK-<version>.zip` from
   [Releases](https://github.com/ryan-flan/DOINK/releases) and unzip it.
2. Copy `AddOns\DOINK` into your WoW AddOns folder, e.g.
   `C:\Program Files (x86)\World of Warcraft\_classic_beta_\Interface\AddOns\`.
   Restart WoW completely (new addons are only picked up on start).
3. Move the `Companion` folder somewhere permanent that you can write to,
   e.g. `Documents\DOINK`, and run `doink.exe`. A gold **D** appears in the
   system tray (click `^` if it's hidden) and the settings window opens.
4. In Discord: *Server Settings → Integrations → Webhooks → New Webhook*,
   choose a channel, *Copy Webhook URL*. Paste it into DOINK's settings,
   click **Save**, then **Send test message** to check it.
5. Optional: tick **Start DOINK with Windows**.

DOINK finds your WoW install by itself; if it can't, the settings window
says so and lets you choose the folder.

To check the whole loop in game: `/doink test levelup`, then `/doink flush`.
A `[TEST]` post should appear a few seconds after the reload.

### The tray icon and settings

**Left-click** the tray icon for settings: webhook, WoW folder, Start with
Windows, and your recent posts. Hover for the last post or the current
problem; if something goes wrong (no webhook, Discord unreachable) you get a
Windows notification once. **Right-click** for Settings, Open log, Open DOINK
folder, Start with Windows and Quit. Closing the settings window keeps DOINK
running in the tray.

**One channel per character?** Set a webhook in game with
`/doink webhook here <url>` on that character. In-game webhooks take priority
over the one in DOINK's settings: per character first, then
`/doink webhook <url>` (all characters), then the settings window.

## What doink.exe does (and doesn't)

- **Reads** `WTF\Account\*\SavedVariables\DOINK.lua` in your WoW folders.
- **Writes** only inside its own folder: `config.toml` (your settings),
  `state.json` (what it has already posted), `doink.log` (capped at ~2 MB).
- **Network:** only HTTPS posts to your Discord webhook. No telemetry, no
  update checks. It doesn't listen on any port.
- **Start with Windows** adds one per-user registry value,
  `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\DOINK`. No admin
  rights. Untick it in the tray menu, or in Task Manager → Startup apps.
- **Realtime (off unless you turn it on):** reads the top and bottom 24 pixel
  rows of the WoW window while it's the active window, looking for the
  addon's strip. Nothing else on screen, never saved, and it watches its own
  CPU and memory use (details below).
- **Uninstall:** untick Start with Windows, quit, delete the folder.

Every release is built from this repository's source by
[GitHub Actions](.github/workflows/release.yml); the build log is public on
the release's workflow run. Check your download against the `.sha256` file on
the release page:

```powershell
Get-FileHash DOINK-v0.6.0.zip -Algorithm SHA256
```

> Windows SmartScreen may warn about `doink.exe` because it isn't code-signed
> (*More info → Run anyway*). Code signing costs money per year; it may come
> later.

## How realtime works (experimental)

WoW only writes addon data on `/reload` or logout, and in the Forever beta
every other way out of the client is shut: addons can't read the combat log,
and the chat and combat log *files* are written minutes or hours late. The
one thing an addon can always do is draw on the screen. So realtime mode
works like this:

1. **In game**, after an event, the addon shows a strip of small black and
   white blocks, a bit like a barcode, in a corner of the screen. It's
   9 pixels tall and up to 1200 wide, shows the event's text encoded as bits,
   and disappears after about two seconds. Nothing shows between events.
2. **The companion** looks at the top and bottom 24 pixel rows of the WoW
   window a few times a second, finds the strip, checks it (every strip
   carries checksums), and posts the event. Typical delay: under a second.

What it does **not** do: it doesn't read the game's memory, doesn't send any
input to the game, and doesn't inject anything into it. It reads pixels that
your own addon put on screen, the same technique the DiscordRichPresence
addon has used for years. We can't speak for Blizzard, which is why it's
opt-in and labelled experimental.

**Cost.** Each look copies two thin bands of the window (about 250 KB) into a
buffer that is allocated once and reused, and decodes them in place; on a
3440-pixel-wide window that's roughly a millisecond, so well under 1% of one
CPU core. The companion measures its own capture time, memory and Windows
handle count while realtime is on, shows them in the settings window, and
**turns realtime off by itself** if captures get slow or memory climbs. The
normal posting path is unaffected either way.

**Reliability.** Realtime is best effort on top of the normal path. If the
companion misses a strip (WoW was behind another window, or something
covered the strip), the event still posts at the next `/reload` or logout,
and nothing ever posts twice.

**Turning it on** takes two steps, because the companion can't reach into the
game:

1. Settings window → tick **Realtime posting**.
2. In game: `/doink realtime on`. Then `/doink realtime test` shows a test
   pattern for ten seconds; the settings window should report it.

If something external sits in that corner (the Discord overlay's voice widget
defaults to the top-left), move the strip: `/doink realtime position
bottomright` (or `topright`, `bottomleft`). Realtime only reads while WoW is
the active window; events that happen while you're alt-tabbed, or with the
UI hidden (Alt-Z), post at the next reload. `/doink realtime off` removes the
strip entirely.

## In-game commands

| Command | |
|---|---|
| `/doink` | Status: queue, webhook, which notifiers are on |
| `/doink enable\|disable <type>` | Turn a notifier on or off |
| `/doink options [type]` | Show settings |
| `/doink set <type> <option> <value>` | Change a setting, e.g. `/doink set loot min_quality 4` |
| `/doink reset <type>` | Back to defaults |
| `/doink webhook <url>` | Webhook for all your characters |
| `/doink webhook here <url>` | Webhook for this character only (e.g. one channel per alt) |
| `/doink webhook [here] clear` | Remove it |
| `/doink test <type>` | Queue a fake event to check your setup |
| `/doink realtime on\|off` | Experimental: show events as a strip for the companion to read live |
| `/doink realtime test` | Show a test pattern for 10 s |
| `/doink realtime position <corner>` | `topleft` (default), `topright`, `bottomleft`, `bottomright` |
| `/doink flush` | Reload now so pending events post |
| `/doink dump [n]`, `/doink debug` | For troubleshooting |

Settings are per character and take effect immediately. Webhook changes reach
the companion on the next reload.

## Notifiers

| Type | Posts when | Options (defaults) |
|---|---|---|
| `level_up` | You level up | `milestones_only` off (on = every 10 levels) |
| `loot` | You loot something good | `min_quality` 3 (rare), `min_vendor_value` 1g (whole stack). Either one is enough |
| `death` | You die | |
| `quest` | You turn in a quest | |
| `boss_kill` | Your group kills a dungeon or raid boss | |
| `skill_up` | A skill reaches a milestone | `milestones_only` on (75/150/225/300) |

Money options accept `1g50s`, `75s`, `30c` or plain copper. Quality: 0 poor,
1 common, 2 uncommon, 3 rare, 4 epic, 5 legendary.

## Companion settings

The settings window covers everyday setup and saves to `config.toml` next to
`doink.exe`. For the rest (`dry_run`, `max_backlog`, `poll_interval`, an
exact `savedvariables_path`), see `config.example.toml`; quit DOINK before
editing the file by hand, then start it again.

The companion remembers what it has posted (`state.json`), so restarting it
never double-posts. The first time it sees a character it posts only the
newest 10 queued events.

## Known limitations

- Posts normally arrive on `/reload` or logout. Realtime mode is experimental
  and only reads while WoW is the active window.
- Deaths don't say what killed you: Forever blocks addons from reading the
  combat log.
- Boss kills aren't confirmed in Forever dungeons yet.

## Development

```
addon/DOINK/       the addon (Lua 5.1)
addon/tests/       offline harness with WoW API stubs
companion/         the companion (Python 3.12+, standard library only)
```

```bash
cd addon && luajit tests/test_addon.lua
cd companion && python3 -m unittest discover -s tests
cd companion && python3 main.py --dry-run     # console mode
cd companion && python main.py --tray          # tray mode (Windows Python)
```

The tray, autostart, single-instance and screen-reading code (`tray.py`,
`autostart.py`, `pixel.py`) talks to Win32 directly through `ctypes`; its
tests run on the Windows CI job, including a capture leak test. The strip
encoder is `addon/DOINK/Transports/Pixel.lua`; `addon/tests/fixtures/` holds
its output for a fixed event, which the Python decoder tests must read back.
The icon is drawn by `companion/assets/make_icon.py`.

Push a `v*` tag to build `doink.exe` and publish a release. Design notes and
the event data contract are in [CLAUDE.md](CLAUDE.md).

## License

[MIT](LICENSE)
