# DOINK: your milestones in guild chat, and in Discord

DOINK is an addon for **World of Warcraft: Forever** that tells your guild
about your adventures. It watches for six kinds of in-game event (level-ups,
loot, deaths, quest turn-ins, boss kills and profession skill-ups) and
announces the ones you choose in guild, officer, party or raid chat the
moment they happen, with real item links:

```
[Guild] [Flano]: Ding! Level 20.
[Guild] [Flano]: Looted [Thunderfury, Blessed Blade of the Windseeker]!
[Guild] [Flano]: Ragnaros down! (Molten Core, 40 players)
[Guild] [Flano]: Killed by Defias Pillager in Westfall.
```

**Discord too.** The free, open-source
[DOINK companion](https://github.com/ryan-flan/DOINK/releases) mirrors the
same events to a Discord channel of your choice as class-coloured embeds
with Wowhead links, so your guild's Discord sees every ding, epic and boss
kill even when nobody is online, and an experimental realtime mode posts
them within a second of them happening. See
[Discord mirroring](#discord-mirroring-with-the-doink-companion).

> **Two parts, one optional.** The addon alone does all the guild chat
> announcements. **Discord posting requires the companion**, a separate
> Windows program you download from the GitHub releases page and run
> alongside the game: WoW addons cannot reach the internet, so without the
> companion nothing goes to Discord. Install the addon from here; get the
> companion from GitHub if you want Discord.

No dependencies and no libraries: install it and it works. Settings are on
a normal options page (Esc → Options → AddOns → DOINK) and can also be set
with `/doink` commands. Status: beta, tracking the Forever beta client.

## Credit: Dink

DOINK is heavily inspired by [Dink](https://github.com/pajlads/DinkPlugin),
the RuneLite plugin by [pajlads](https://github.com/pajlads) that posts your
Old School RuneScape achievements to Discord. The idea of an addon that
notices your milestones and tells your friends, the Discord embeds, the
per-event toggles and the name all come from Dink. DOINK is not affiliated
with the Dink project; it's a from-scratch WoW take on the same idea, built
within what a WoW addon is allowed to do. If you play OSRS, go use Dink.

## Install

Install **DOINK** from CurseForge or Wago Addons (or through an addon
manager such as WowUp), or download `DOINK-<version>.zip` from
[Releases](https://github.com/ryan-flan/DOINK/releases) and copy
`AddOns\DOINK` into your WoW AddOns folder, e.g.
`C:\Program Files (x86)\World of Warcraft\_classic_beta_\Interface\AddOns\`.
Restart WoW completely (new addons are only picked up on start).

That's it for guild chat. `/doink announce test` whispers you a sample of
each announcement so you can see the wording. For Discord, you also need
the companion: see
[Discord mirroring](#discord-mirroring-with-the-doink-companion).

## Announcements

Every event type, what triggers it, the exact wording, and the default rule:

| Event | Triggered by | Chat line | Announced by default |
|---|---|---|---|
| Level-up | `PLAYER_LEVEL_UP` | `Ding! Level 20.` / `Ding! Level 60 - max level!` | Levels 10, 20, 30, 40, 50 and 60 only |
| Loot | Your own `You receive loot:` message | `Looted [item link]!` (`x3` appended for stacks) | Epic quality or better |
| Death | `PLAYER_DEAD`, killer from the client's death recap | `Killed by Defias Pillager in Moonbrook, Westfall.`; falling, drowning, fatigue, fire, lava and slime get their own lines (`Forgot I couldn't fly. Died in Thousand Needles.`); `Died in Moonbrook, Westfall.` when the recap is empty | Yes |
| Boss kill | `ENCOUNTER_END` with success | `Ragnaros down! (Molten Core, 40 players)` | Yes (kills only, never wipes) |
| Quest turn-in | `QUEST_TURNED_IN` | `Completed quest: A Threat Within.` | No |
| Skill-up | `Your skill in X has increased to N.` | `Blacksmithing maxed at 300!` / `Blacksmithing 150/225.` | No |

The defaults are deliberately conservative because guild chat is shared.
Each rule can be changed per character:

- Level-up: `milestones` (default), `all`, `off`
- Loot: minimum quality `any`, `uncommon`, `rare`, `epic` (default), `legendary`, or `off`
- Skill-up: `max` (only when a skill reaches its cap), `milestones` (75/150/225/300), `all`, `off` (default)
- Death, boss kill, quest: `on` or `off`

Channels: `guild` (default), `officer`, `party`, `raid`, or `off`. If you are
not in a guild (or group) nothing is sent and `/doink` says so. Say, yell and
public channels such as General are not offered: Blizzard only allows addons
to send those in response to a key press.

Lines are sent through the normal `SendChatMessage` API, so they appear
exactly like a line you typed yourself.

### Built-in protection against spam (not configurable)

- At most one line every two seconds, and at most eight lines per minute. If
  more events happen than that (a chest full of epics), the extra lines are
  dropped and a message tells **you**, privately, what was dropped.
- Test commands never post to the guild. `/doink test <type>` and
  `/doink announce test` whisper the sample lines to **you**, marked
  `[test]`, so you can check the wording without anyone else seeing it.
- Lines are cut at the 255-character chat limit.

## Commands

All commands start with `/doink`. Typing `/doink` alone prints the status:
addon version, character, how many events are queued, the announcement
channel and rules, the Discord webhook state (never the full URL), the
realtime state, and which event types are enabled.

| Command | What it does |
|---|---|
| `/doink config` | Open the settings page (Esc → Options → AddOns → DOINK) |
| `/doink announce` | Show the announcement channel and rules |
| `/doink announce guild` / `officer` / `party` / `raid` / `off` | Choose where announcements go |
| `/doink announce loot rare` | Change a rule (any event type, values as listed above) |
| `/doink announce test` | Whisper yourself a sample of every announcement, with whether the current rules would send it |
| `/doink enable <type>` / `disable <type>` | Turn an event type off entirely (`level_up`, `loot`, `death`, `quest`, `boss_kill`, `skill_up`) |
| `/doink options [type]` | Show the event settings (for example the loot quality and vendor-value thresholds used for Discord) |
| `/doink set <type> <option> <value>` | Change one, e.g. `/doink set loot min_quality 4` |
| `/doink reset <type>` | Back to defaults |
| `/doink test <type>` | Generate a fake event of that type (whispered to you; also queued for the companion, marked as a test) |
| `/doink dump [n]` | Print the last n queued events |
| `/doink debug` | Toggle printing of every event the addon sees, for troubleshooting |
| `/doink flush` | Reload the UI so saved variables are written |
| `/doink webhook [here] <url>` / `clear` | Only for the Discord companion: store a webhook URL account-wide, or for this character |
| `/doink realtime on` / `off` / `test` / `position <corner>` | Only for the Discord companion's experimental realtime mode |

Settings are per character and take effect immediately, whether changed on
the settings page or by command; the two are the same settings.

## Compatibility

- Built for World of Warcraft: Forever (beta client 1.60.1, Interface
  16001). It uses the Classic-era API only.
- Chat patterns are built from the game's own strings (`LOOT_ITEM_SELF`,
  `SKILL_RANK_UP`), so loot and skill detection work on any client language.
- Forever surnames are supported: characters are tracked by full name, and
  whispers use the full name.

## Known limitations

- The killer in a death announcement comes from the client's death recap
  (the same data as the "Death Recap" button). Addons cannot read the
  combat log in Forever, so if the recap is empty the line omits the killer.
- Announcements can't be sent to Blizzard Communities yet.
- Boss-kill announcements rely on `ENCOUNTER_END`, which hasn't been
  confirmed in Forever dungeons during the beta.

## Discord mirroring with the DOINK companion

**Required for Discord.** Nothing in this section works with the addon
alone. Discord posting needs the DOINK companion running on your Windows
PC, because WoW addons cannot access the network. The addon's guild chat
announcements need nothing extra.

Everything the addon announces can also land in Discord:

> **Flano Wren looted Thunderfury, Blessed Blade of the Windseeker**
> Vendor value: 12g 34s
> Flano Wren-Whatever

(an embed in the item's quality colour, with the item name linking to
Wowhead; level-ups and deaths use your class colour)

WoW addons cannot access the network, so Discord posting is done by a
separate, optional program, the **DOINK companion**. Download
`DOINK-<version>.zip` from the
[GitHub releases page](https://github.com/ryan-flan/DOINK/releases): it
contains the companion (`Companion\doink.exe`) and a copy of the addon.
The companion is not on the addon sites and is not required for anything
above.

What it is: a Windows program (`doink.exe`, Python packaged with
PyInstaller, source in this repository, MIT) that sits in the system tray.
Every two seconds it checks whether the game has written `DOINK.lua`; when
it has, it reads the event buffer and posts each new event to a Discord
webhook you provide, as an embed (class-coloured, with a Wowhead link for
items and quests). It remembers what it has posted so nothing is posted
twice. It reads only `DOINK.lua`, writes only its own three files
(settings, posting history, log) in its own folder, and makes only HTTPS
requests to the Discord webhook URL you paste into it.

> **Discord posts arrive when WoW saves addon data:** on `/reload`, logout,
> or exit. That's a WoW limitation, not a setting. Play normally and your
> session posts when you log out, or type `/doink flush` to post right away.
> There is also an opt-in, experimental **realtime** mode that posts while
> you play; see [How realtime works](#how-realtime-works-experimental).
> Guild chat announcements are always instant.

### Setting it up

1. Download `DOINK-<version>.zip` from
   [Releases](https://github.com/ryan-flan/DOINK/releases), move its
   `Companion` folder somewhere permanent that you can write to, e.g.
   `Documents\DOINK`, and run `doink.exe`. A gold **D** appears in the
   system tray (click `^` if it's hidden) and the settings window opens.
2. In Discord: *Server Settings → Integrations → Webhooks → New Webhook*,
   choose a channel, *Copy Webhook URL*. Paste it into DOINK's settings,
   click **Save**, then **Send test message** to check it.
3. Optional: tick **Start DOINK with Windows**.

DOINK finds your WoW install by itself; if it can't, the settings window
says so and lets you choose the folder.

To check the whole loop in game: `/doink test levelup`, then `/doink flush`.
A `[TEST]` post should appear a few seconds after the reload.

**Left-click** the tray icon for settings: webhook, WoW folder, Start with
Windows, and your recent posts. Hover for the last post or the current
problem; if something goes wrong (no webhook, Discord unreachable) you get a
Windows notification once. **Right-click** for Settings, Open log, Open DOINK
folder, Start with Windows and Quit. Closing the settings window keeps DOINK
running in the tray.

**One channel per character?** Set a webhook in game with
`/doink webhook here <url>` on that character. In-game webhooks take priority
over the one in DOINK's settings: per character first, then
`/doink webhook <url>` (all characters), then the settings window. Webhook
changes reach the companion on the next reload.

### Notifiers (what gets recorded for Discord)

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

### Companion settings

The settings window covers everyday setup and saves to `config.toml` next to
`doink.exe`. For the rest (`dry_run`, `max_backlog`, `poll_interval`, an
exact `savedvariables_path`), see `config.example.toml`; quit DOINK before
editing the file by hand, then start it again.

The companion remembers what it has posted (`state.json`), so restarting it
never double-posts. The first time it sees a character it posts only the
newest 10 queued events.

### What doink.exe does (and doesn't)

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
Get-FileHash DOINK-v0.9.5.zip -Algorithm SHA256
```

> Windows SmartScreen may warn about `doink.exe` because it isn't code-signed
> (*More info → Run anyway*). Code signing costs money per year; it may come
> later.

### How realtime works (experimental)

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

## How it works: what is in the package

One folder, `DOINK`, 13 files, about 1,700 lines of Lua, no third-party code:

| File | Purpose |
|---|---|
| `DOINK.toc` | Addon manifest. `## Interface: 16001`, `## SavedVariables: DOINKDB`. |
| `Core.lua` | Startup, saved-variables handling, the `/doink` slash command, event dispatch. |
| `Defaults.lua` | Default settings and the allowed values for each setting. |
| `Announce.lua` | Chat announcements: wording, rules, rate limiting. |
| `Options.lua` | The settings page, built on the game's own Settings API. |
| `Json.lua` | A minimal JSON encoder used to store events for the optional companion. |
| `Notifiers/LevelUp.lua` | Listens to `PLAYER_LEVEL_UP`. |
| `Notifiers/Loot.lua` | Listens to `CHAT_MSG_LOOT` and `GET_ITEM_INFO_RECEIVED`. |
| `Notifiers/Death.lua` | Listens to `PLAYER_DEAD`. |
| `Notifiers/Quest.lua` | Listens to `QUEST_COMPLETE` and `QUEST_TURNED_IN`. |
| `Notifiers/BossKill.lua` | Listens to `ENCOUNTER_END`. |
| `Notifiers/SkillUp.lua` | Listens to `CHAT_MSG_SKILL`. |
| `Transports/Pixel.lua` | The optional, off-by-default "realtime" strip for the companion. Creates nothing unless enabled. |

The addon registers only the events listed above (plus `ADDON_LOADED` and
`PLAYER_LOGIN`). It does not register the combat log event, which Forever
protects, and it does not hook or replace any Blizzard function. It makes
no network connections (addons can't) and reads nothing from disk.

## How it works: what the addon stores

Everything is kept in one saved variable, `DOINKDB`, written by the game to
`WTF\Account\<account>\SavedVariables\DOINK.lua` on `/reload` and logout:

- Per character: your settings (only the ones you changed), a counter, and a
  ring buffer of the last 500 events as JSON text (type, time, character,
  realm, class, level, and event details such as item id and name). This
  buffer exists for the optional companion; the addon itself never reads it
  back.
- Account-wide: the debug flag, the realtime settings (off by default), and
  any Discord webhook URL you set with `/doink webhook`. If you never set
  one, nothing is stored there.

The addon does not store chat, names of other players, or anything about
other characters.

## Development, source and AI use

Source code, issue tracker and the companion: https://github.com/ryan-flan/DOINK
(MIT license). Every uploaded file (CurseForge and Wago Addons) is built
from a tagged commit of that repository by GitHub Actions using the BigWigs
packager, so the uploaded zip matches the public source exactly. The
changelog for each version is generated from the commit history and attached
to every file.

The repository includes an automated test suite for the addon (run outside
the game against a stub of the WoW API) covering every notifier, the
announcement rules, the rate limiter and the slash commands; it runs on
every change.

**AI use disclaimer:** this addon and its companion were written with the
help of an AI assistant (Anthropic's Claude), working under the direction of
the developer, who decided what to build, reviewed the code, and tested each
release in the Forever beta client. Every WoW API behaviour the addon relies
on was verified in the game rather than assumed, and the full source is
public for anyone to review.

Bugs and requests: https://github.com/ryan-flan/DOINK/issues.

### Building and testing

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

Push a `v*` tag to build `doink.exe`, publish a GitHub release and upload the
addon to CurseForge and Wago Addons. Design notes and the event data
contract are in [CLAUDE.md](CLAUDE.md).

## License

[MIT](LICENSE)
