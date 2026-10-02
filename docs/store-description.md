# DOINK: announces your character's milestones in guild chat

DOINK watches for six kinds of in-game event (level-ups, loot, deaths, quest
turn-ins, boss kills and profession skill-ups) and announces the ones you
choose in guild, officer, party or raid chat the moment they happen, with
real item links:

```
[Guild] [Paul Hebbs]: Ding! Level 20.
[Guild] [Paul Hebbs]: Looted [Thunderfury, Blessed Blade of the Windseeker]!
[Guild] [Paul Hebbs]: Ragnaros down! (Molten Core, 40 players)
[Guild] [Paul Hebbs]: Died in Westfall.
```

No dependencies, no libraries, no setup window: install it and it works.
Optionally, a separate open-source companion program can mirror the same
events to a Discord channel (see "Optional Discord mirroring").

## Announcements

Every event type, what triggers it, the exact wording, and the default rule:

| Event | Triggered by | Chat line | Announced by default |
|---|---|---|---|
| Level-up | `PLAYER_LEVEL_UP` | `Ding! Level 20.` / `Ding! Level 60 - max level!` | Levels 10, 20, 30, 40, 50 and 60 only |
| Loot | Your own `You receive loot:` message | `Looted [item link]!` (`x3` appended for stacks) | Epic quality or better |
| Death | `PLAYER_DEAD` | `Died in Moonbrook, Westfall.` | Yes |
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

## Compatibility

- Built for World of Warcraft: Forever (beta client 1.60.1, Interface
  16001). It uses the Classic-era API only.
- Chat patterns are built from the game's own strings (`LOOT_ITEM_SELF`,
  `SKILL_RANK_UP`), so loot and skill detection work on any client language.
- Forever surnames are supported: characters are tracked by full name, and
  whispers use the full name.

## Known limitations

- Deaths don't say what killed you: Forever does not let addons read the
  combat log.
- Announcements can't be sent to Blizzard Communities yet.
- Boss-kill announcements rely on `ENCOUNTER_END`, which hasn't been
  confirmed in Forever dungeons during the beta.

## Optional Discord mirroring (separate download)

WoW addons cannot access the network, so Discord posting is done by a
separate program, the **DOINK companion**, downloaded from the GitHub
releases page (not from CurseForge). It is not required for anything above.

What it is: a Windows program (`doink.exe`, Python packaged with
PyInstaller, source in the same repository, MIT) that sits in the system
tray. Every two seconds it checks whether the game has written
`DOINK.lua`; when it has, it reads the event buffer and posts each new event
to a Discord webhook you provide, as an embed (class-coloured, with a
Wowhead link for items and quests). It remembers what it has posted so
nothing is posted twice. It reads only `DOINK.lua`, writes only its own
three files (settings, posting history, log) in its own folder, and makes
only HTTPS requests to the Discord webhook URL you paste into it.

Because the game writes saved variables only on `/reload` or logout, Discord
posts normally arrive then. The companion also has an opt-in, experimental
"realtime" mode: with `/doink realtime on` the addon shows a strip of small
black and white blocks (9 pixels tall) in a screen corner for about two
seconds after each event, and the companion reads that strip off the game
window and posts the event within about a second. The strip is never shown
unless you turn this on, and `/doink realtime off` removes it. Full details,
including exactly what the companion reads from the screen, are in the
README on GitHub under "How realtime works".

## How it works: what is in the package

One folder, `DOINK`, 12 files, about 1,500 lines of Lua, no third-party code:

| File | Purpose |
|---|---|
| `DOINK.toc` | Addon manifest. `## Interface: 16001`, `## SavedVariables: DOINKDB`. |
| `Core.lua` | Startup, saved-variables handling, the `/doink` slash command, event dispatch. |
| `Defaults.lua` | Default settings and the allowed values for each setting. |
| `Announce.lua` | Chat announcements: wording, rules, rate limiting. |
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
(MIT license). Every CurseForge file is built from a tagged commit of that
repository by GitHub Actions using the BigWigs packager, so the uploaded
zip matches the public source exactly. The changelog for each version is
generated from the commit history and attached to every file.

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
