# DOINK

An addon for **World of Warcraft: Forever** that tells your guild about your
adventures: level milestones, rare loot, deaths and boss kills, announced in
guild chat as they happen, with real item links. Optionally, a small
companion app mirrors everything to a Discord channel too. Inspired by
[Dink](https://github.com/pajlads/DinkPlugin) for RuneLite. Status: beta,
tracking the Forever beta client.

## How it works

**The addon on its own** watches for events and announces them in chat:

```
[Guild] Flano: Ding! Level 20.
[Guild] Flano: Looted [Thunderfury, Blessed Blade of the Windseeker]!
[Guild] Flano: Ragnaros down! (Molten Core, 40 players)
```

Guild chat by default; officer, party or raid chat if you prefer. Nothing
leaves the game and there is nothing to install besides the addon.

**With the companion** (`doink.exe`, optional), the same events also go to a
Discord channel. WoW addons can't talk to the internet, so the addon queues
events in its saved data and the companion, running alongside the game,
reads that queue and posts each event to your Discord webhook.

> **Discord posts arrive when WoW saves addon data:** on `/reload`, logout,
> or exit. That's a WoW limitation, not a setting. Play normally and your
> session posts when you log out, or type `/doink flush` to post right away.
> There is also an opt-in, experimental **realtime** mode that posts while
> you play; see [How realtime works](#how-realtime-works-experimental).
> Guild chat announcements are always instant.

## Install

1. Download `DOINK-<version>.zip` from
   [Releases](https://github.com/ryan-flan/DOINK/releases) (or install
   **DOINK** from CurseForge) and copy `AddOns\DOINK` into your WoW AddOns
   folder, e.g.
   `C:\Program Files (x86)\World of Warcraft\_classic_beta_\Interface\AddOns\`.
   Restart WoW completely (new addons are only picked up on start).
2. That's it for guild announcements. `/doink announce test` whispers you a
   sample of each one so you can see the wording. Settings live in the
   game's options (Esc → Options → AddOns → DOINK, or `/doink config`) and
   in the `/doink` commands (see [Announcements](#announcements)).

### Optional: Discord

3. Move the `Companion` folder from the zip somewhere permanent that you can
   write to, e.g. `Documents\DOINK`, and run `doink.exe`. A gold **D**
   appears in the system tray (click `^` if it's hidden) and the settings
   window opens.
4. In Discord: *Server Settings → Integrations → Webhooks → New Webhook*,
   choose a channel, *Copy Webhook URL*. Paste it into DOINK's settings,
   click **Save**, then **Send test message** to check it.
5. Optional: tick **Start DOINK with Windows**.

DOINK finds your WoW install by itself; if it can't, the settings window
says so and lets you choose the folder.

To check the whole loop in game: `/doink test levelup`, then `/doink flush`.
A `[TEST]` post should appear a few seconds after the reload.

## Announcements

What goes to chat is deliberately stricter than what goes to Discord,
because guild chat is shared. Defaults:

| Event | Announced by default | Rule values |
|---|---|---|
| Level up | milestones: 10, 20 … 60 | `milestones`, `all`, `off` |
| Loot | epic or better | `any`, `uncommon`, `rare`, `epic`, `legendary`, `off` |
| Death | yes | `on`, `off` |
| Boss kill | yes | `on`, `off` |
| Quest turn-in | no | `on`, `off` |
| Skill-up | no | `max`, `milestones`, `all`, `off` |

- `/doink announce guild` (default), `officer`, `party`, `raid` or `off`.
- `/doink announce loot rare`, `/doink announce quest on`, and so on.
- `/doink announce test` whispers you a sample of every line, with whether
  the current rules would announce it. `/doink test <type>` events are
  whispered to you too, never to the guild.
- Lines go out at most one every two seconds and eight a minute; anything
  beyond that is dropped and you're told, so a loot burst can't flood
  anyone. Say, yell and public channels are blocked for addons by Blizzard,
  so those aren't options.

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
Get-FileHash DOINK-v0.8.0.zip -Algorithm SHA256
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
buffer that is allocated once and reused, and decodes them in place.
Measured on the author's PC with a 3438×1408 window: about 8 looks a second,
roughly 1% of one CPU core, 46 MB of memory, and a flat handle count; each
look takes ~15 ms of waiting on the graphics driver but only ~1 ms of CPU.
The companion measures its own capture cost, memory and Windows handle count
while realtime is on, shows them in the settings window, and **turns
realtime off by itself** if captures start costing real CPU, handles pile
up, or memory keeps climbing. The normal posting path is unaffected either
way.

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
| `/doink config` | Open the settings page (same as Esc → Options → AddOns → DOINK) |
| `/doink enable\|disable <type>` | Turn a notifier on or off |
| `/doink options [type]` | Show settings |
| `/doink set <type> <option> <value>` | Change a setting, e.g. `/doink set loot min_quality 4` |
| `/doink reset <type>` | Back to defaults |
| `/doink webhook <url>` | Webhook for all your characters |
| `/doink webhook here <url>` | Webhook for this character only (e.g. one channel per alt) |
| `/doink webhook [here] clear` | Remove it |
| `/doink announce [channel]` | Show the chat announcement rules, or pick `guild`, `officer`, `party`, `raid`, `off` |
| `/doink announce <type> <rule>` | Change a rule, e.g. `/doink announce loot rare` |
| `/doink announce test` | Whisper yourself a sample of every announcement |
| `/doink test <type>` | Queue a fake event to check your setup (whispered to you, never announced) |
| `/doink realtime on\|off` | Experimental: show events as a strip for the companion to read live |
| `/doink realtime test` | Show a test pattern for 10 s |
| `/doink realtime position <corner>` | `topleft` (default), `topright`, `bottomleft`, `bottomright` |
| `/doink flush` | Reload now so pending events post |
| `/doink dump [n]`, `/doink debug` | For troubleshooting |

Settings are per character and take effect immediately, whether changed on
the settings page or by command; the two are the same settings. Webhook
changes reach the companion on the next reload.

## Notifiers (Discord)

These decide which events are recorded for Discord; the chat announcement
rules above are applied on top.

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

- Discord posts normally arrive on `/reload` or logout (chat announcements
  are instant). Realtime mode is experimental and only reads while WoW is
  the active window.
- Chat announcements can't go to communities yet; Forever has the API, but
  it's untested until there is a community to test with.
- The killer in a death line comes from the client's death recap. If the
  recap is empty when you die, the line is just "Died in <zone>."
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
