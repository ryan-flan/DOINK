# DOINK — Discord notifications for World of Warcraft: Forever

DOINK is "Dink for WoW": a WoW addon plus a small companion app that posts
in-game events (level-ups, rare loot, deaths, quest turn-ins, boss kills,
skill-ups) to a Discord webhook. Inspiration: https://github.com/pajlads/DinkPlugin

Target game: **World of Warcraft: Forever** (Blizzard's Classic+, launches
2026-11-04; beta runs until 2026-10-21). The addon API is the Classic Era API.

## The one hard constraint

WoW addons run in a sandboxed Lua 5.1 environment. They **cannot** make HTTP
requests, open sockets, or write arbitrary files. The only persistence is
SavedVariables, which the client writes **only on `/reload` or logout**.

Therefore DOINK is two parts:

1. **Addon (Lua)** — detects events, appends them to a queue in SavedVariables.
2. **Companion (Python, runs on the Windows gaming PC)** — reads the queue and
   POSTs to Discord.

The addon is transport-agnostic. The companion decides how it learns about
events. v1 uses a file watcher; later versions may add a combat-log tailer or a
pixel-encoding screen reader for realtime. **Never** write addon code that
assumes a particular transport.

## Repo layout

```
DOINK/
├── CLAUDE.md
├── README.md
├── .gitattributes            # * text=auto eol=lf
├── addon/DOINK/              # junctioned into <WoW>/<flavor>/Interface/AddOns/DOINK
│   ├── DOINK.toc
│   ├── Core.lua              # init, SavedVariables, queue, slash commands
│   ├── Json.lua              # minimal JSON encoder (strings, numbers, bools, tables)
│   ├── Defaults.lua          # default config table
│   └── Notifiers/
│       ├── LevelUp.lua
│       ├── Loot.lua
│       ├── Death.lua
│       ├── Quest.lua
│       ├── BossKill.lua
│       └── SkillUp.lua
└── companion/
    ├── pyproject.toml
    ├── main.py
    ├── config.example.toml
    └── doink/
        ├── watcher.py        # polls SavedVariables file mtime
        ├── parser.py         # extracts event JSON strings from the Lua file
        ├── discord.py        # embed builders + webhook POST
        └── state.py          # last-seen seq per character, persisted locally
```

## Data contract (both halves depend on this — do not change casually)

The addon stores **each event as a JSON string** inside a Lua table. Parsing
Lua table syntax from Python is painful; a Lua table of strings is trivial
(regex the quoted strings, `json.loads` each). One small Lua encoder saves a
whole Lua parser in Python.

SavedVariables global: `DOINKDB`

```lua
DOINKDB = {
  version = 1,
  chars = {
    ["Flano-Whatever"] = {
      seq = 42,                 -- last seq issued for this char
      config = { ... },         -- per-char config overrides
      events = {                -- ring buffer, newest last, max 500 entries
        '{"seq":41,"ts":1759300000,...}',
        '{"seq":42,"ts":1759300100,...}',
      },
    },
  },
}
```

Event JSON schema:

```json
{
  "seq":   42,
  "ts":    1759300000,
  "char":  "Flano",
  "realm": "Whatever",
  "class": "WARRIOR",
  "level": 20,
  "type":  "level_up",
  "data":  { ... type-specific ... },
  "test":  true                 // optional; only present on /doink test events
}
```

Rules:
- `test` is present (and `true`) only for events from `/doink test`. The
  companion should still post them, but label them as tests.
- `seq` is per-character, monotonic, never reused. The companion dedupes on it.
- `ts` is `time()` (Unix seconds, server-ish time).
- Ring buffer: when `#events > 500`, drop from the front.
- `data` shapes per type:
  - `level_up`:  `{"level": 20}`
  - `loot`:      `{"item_id": 123, "link": "|cff...|h[Name]|h|r", "name": "...", "quality": 3, "qty": 1, "vendor_value": 1234}`
  - `death`:     `{"zone": "Westfall", "subzone": "...", "killer": "Defias Pillager"|null}`
  - `quest`:     `{"quest_id": 123, "title": "...", "xp": 1200}`
  - `boss_kill`: `{"encounter_id": 123, "name": "...", "instance": "...", "difficulty": 1, "success": true, "group_size": 5}`
  - `skill_up`:  `{"skill": "Blacksmithing", "rank": 150, "max_rank": 150}`

## Notifiers (v1)

| Type        | Trigger event              | Default filter                                   |
|-------------|----------------------------|--------------------------------------------------|
| `level_up`  | `PLAYER_LEVEL_UP`          | every level (option: milestones 10/20/.../60)    |
| `loot`      | `CHAT_MSG_LOOT`            | quality >= 3 (rare/blue) OR vendor value >= N    |
| `death`     | `PLAYER_DEAD`              | always                                           |
| `quest`     | `QUEST_TURNED_IN`          | always                                           |
| `boss_kill` | `ENCOUNTER_END`            | success only                                     |
| `skill_up`  | `CHAT_MSG_SKILL`           | milestones only (75/150/225/300)                 |

Deferred to v1.1 pending beta findings: achievements (vanilla had none; Forever
may add them), rare mob kills (combat log `UNIT_DIED` + classification).

## Addon conventions

- File header pattern: `local ADDON, ns = ...` — share state via `ns`, never
  pollute globals except `DOINKDB` (SavedVariables) and the `/doink` slash cmd.
- One notifier per file. Each exposes `ns.Notifiers.<Name> = { type = "<type>",
  events = {...}, OnEvent = function(event, ...) end, Test = function() return
  data end }` and calls `ns:Emit(type, data)`. `Test` returns a fixture `data`
  table for `/doink test`, which bypasses the notifier's filter but not the
  enable flag.
- Options: read with `ns:GetOption(type, key)`. Per-char `config` stores only
  overrides; defaults live in `Defaults.lua` and are never copied into the DB.
- JSON `null`: use the `ns.Json.null` sentinel (Lua tables can't hold `nil`).
- `Core.lua` owns: `ADDON_LOADED` / `PLAYER_LOGIN` lifecycle, DB init &
  migration, `ns:Emit()` (builds envelope, assigns seq, JSON-encodes, appends,
  trims ring buffer), event dispatch to notifiers, slash commands.
- No Ace3 or other libraries in v1. Learn the raw API first; libraries can come
  in the config/UI phase.
- Lua 5.1 semantics. No `goto`, no integer division, no `//`.
- Guard everything that can be nil (`GetItemInfo` may return nil on first call
  — use `C_Item.GetItemInfo` where available and retry via `GET_ITEM_INFO_RECEIVED`
  if needed).
- Register **all** notifier files in the TOC up front even if stubbed: TOC
  changes need a full client restart, Lua changes only need `/reload`.

### Slash commands (implement in M1, these are the test harness)

```
/doink                   show status: enabled notifiers, queue length, last seq
/doink test <type>       emit a fake event of <type> through the real pipeline
/doink dump [n]          print the last n queue entries to chat
/doink debug             toggle verbose logging of every event notifiers see
/doink flush             ReloadUI() to force SavedVariables write
/doink enable|disable <type>
```

## Companion conventions

- Python 3.12+, run with **Windows Python** (the file lives on NTFS; inotify
  across WSL `/mnt/c` is unreliable). Developer may run git/tooling from WSL.
- Poll the SavedVariables file's mtime every ~2s. No OS file-watch hooks.
- On change: parse → events with `seq > state[char].last_seen` → POST in order →
  persist state. Never re-post on restart.
- Parsing: find the `events = { ... }` blocks, extract Lua string literals
  (handle `\"` and `\\` escapes), `json.loads` each. Skip unparseable entries
  with a warning rather than crashing.
- Discord: one embed per event, colour by class (use the standard WoW class
  colours), Wowhead link for items (`https://www.wowhead.com/classic/item=<id>`
  — verify the Forever Wowhead URL scheme), footer `Char-Realm`. Respect the
  webhook rate limit (~30/min); batch if many events arrive from one reload.
- `--dry-run` prints embeds to stdout instead of POSTing.
- Config via `config.toml`: `savedvariables_path`, `webhook_url`, per-type
  enable flags, `dry_run`. Ship `config.example.toml`, gitignore `config.toml`.
- Later: PyInstaller single-exe build. Not v1.

## Dev setup (reference)

- Repo lives at `C:\dev\DOINK` (WSL: `/mnt/c/dev/DOINK`).
- Addon folder is junctioned:
  `mklink /J "<WoW>\<flavor>\Interface\AddOns\DOINK" "C:\dev\DOINK\addon\DOINK"`
- In-game helpers: BugSack + BugGrabber (errors), DevTool (`/dev`), built-in
  `/etrace`, `/dump`, `/fstack`, `/console scriptErrors 1`.
- SavedVariables path:
  `<WoW>\<flavor>\WTF\Account\<ACCOUNT>\SavedVariables\DOINK.lua`
- `git` and `gh` are installed only in WSL, not on the Windows PATH. From
  PowerShell: `wsl -e sh -c "cd /mnt/c/dev/DOINK && git ..."`.
- Remote: https://github.com/ryan-flan/DOINK (private), default branch `main`.

## Commits

[Conventional Commits](https://www.conventionalcommits.org):

```
<type>(<scope>): <imperative summary, lowercase, no trailing period>

<optional body: what and why, wrapped at ~72>
```

- Types: `feat`, `fix`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`,
  `perf`, `style`.
- Scopes: `addon`, `companion`, `contract` (the data contract above). Omit the
  scope for repo-wide changes (e.g. `docs: update milestones`).
- Any change to the data contract uses `!` and a `BREAKING CHANGE:` footer,
  e.g. `feat(contract)!: rename quest.xp to quest.xp_reward`. Both halves
  depend on it.
- One logical change per commit. Don't mix addon and companion changes unless
  they're a single contract change that touches both.
- History is changelog fodder (BigWigs packager later), so write summaries for
  a player reading release notes, not for yourself.

## Beta facts to fill in (TODO — do not guess these)

- [x] Flavor folder name: `_classic_beta_` (`.flavor.info`: `wow_classic_beta`).
  Install: `C:\Program Files (x86)\World of Warcraft\`. Expect a different
  folder at launch.
- [x] TOC `## Interface:` version: `16001` (client `1.60.1 70124`, Sep 29 2026).
- [x] Achievements present? API exists (`GetTotalAchievementPoints()` → `0`),
  not enabled in beta yet. Exploration achievements have been shown. v1.1.
- [ ] `ENCOUNTER_END` fires in Forever dungeons? ______
- [ ] `CHAT_MSG_LOOT` self-loot message format sample: ______
- [x] SavedVariables string format: events are written as double-quoted Lua
  strings with `\"` escapes, one per line:
  `"{\"char\":\"Paul\",...,\"type\":\"level_up\"}",`

## Milestones

- **M1 — Skeleton. ✅ Done.** TOC (all files listed), Core, Json, Defaults, LevelUp
  notifier, all slash commands. Done when `/doink test levelup` → `/reload`
  produces a `level_up` JSON string in `DOINK.lua`.
- **M2 — First post.** Companion watcher + parser + discord + state, posting
  the level-up embed. Done when the end-to-end loop works.
- **M3 — Notifiers.** Loot, Death, Quest, BossKill, SkillUp following the
  LevelUp pattern. Each with a `/doink test` fixture.
- **M4 — Polish.** Config via slash commands, README, `.gitattributes`,
  PyInstaller build.
- **Later:** combat-log tailer (realtime deaths/boss kills), pixel bridge
  (realtime everything), options UI (Ace3), CurseForge/Wago packaging via the
  BigWigs packager action, rare kills, achievements.

## Working style

- Implement one milestone per request. Don't run ahead.
- When the WoW API is uncertain, say so and propose how to verify in-game via
  `/etrace` or `/dump` rather than guessing a signature.
- Keep the addon side small and readable; the developer is learning the WoW API
  through this code.
