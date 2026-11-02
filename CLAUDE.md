# DOINK — Discord notifications for World of Warcraft: Forever

DOINK is "Dink for WoW": a WoW addon that announces in-game events
(level-ups, rare loot, deaths, quest turn-ins, boss kills, skill-ups) in
guild chat, plus an **optional** companion app that mirrors them to a Discord
webhook. Since v0.7.0 the addon is the product and works alone; Discord is
the extra. Inspiration: https://github.com/pajlads/DinkPlugin

Target game: **World of Warcraft: Forever** (Blizzard's Classic+, launches
2026-11-04; beta runs until 2026-10-21). The addon API is the Classic Era API.

## The one hard constraint

WoW addons run in a sandboxed Lua 5.1 environment. They **cannot** make HTTP
requests, open sockets, or write arbitrary files. The only persistence is
SavedVariables, which the client writes **only on `/reload` or logout**.

Therefore DOINK is two parts:

1. **Addon (Lua)** — detects events, announces them in chat (`Announce.lua`,
   needs nothing else) and appends them to a queue in SavedVariables.
2. **Companion (Go, runs on the gaming PC: Windows, or Linux for Lutris/Proton; optional)** — reads
   the queue and POSTs to Discord.

The addon is transport-agnostic. The companion decides how it learns about
events: a file watcher on SavedVariables, plus (since v0.14) the
screenshots the addon takes for realtime. **Never** write addon code that
assumes a particular transport.

## Repo layout

```
DOINK/
├── CLAUDE.md
├── README.md                 # user-facing install + commands
├── .gitattributes            # * text=auto eol=lf
├── .github/workflows/        # ci.yml: both test suites; release.yml: doink.exe + Linux doink on v* tags
├── addon/tests/test_addon.lua  # offline harness: cd addon && luajit tests/test_addon.lua
├── addon/DOINK/              # junctioned into <WoW>/<flavor>/Interface/AddOns/DOINK
│   ├── DOINK.toc
│   ├── Core.lua              # init, SavedVariables, queue, slash commands
│   ├── Json.lua              # minimal JSON encoder (strings, numbers, bools, tables)
│   ├── Defaults.lua          # default config table + ns.Choices for string options
│   ├── Announce.lua          # in-game chat announcements (guild/officer/party/raid)
│   ├── Options.lua           # settings page via the client's Settings API (no library)
│   ├── Transports/Screenshot.lua # realtime: grid + Screenshot() (off by default)
│   └── Notifiers/
│       ├── LevelUp.lua
│       ├── Loot.lua
│       ├── Death.lua
│       ├── Quest.lua
│       ├── BossKill.lua
│       └── SkillUp.lua
└── companion/                # Go module github.com/ryan-flan/DOINK/companion
    ├── go.mod                # go 1.23; deps: BurntSushi/toml, golang.org/x/sys, jchv/go-webview2
    ├── config.example.toml
    ├── assets/               # doink.ico, doink-update.ico (embedded), make_icon.py
    ├── winres/winres.json    # exe icon + version info (go-winres, CI only)
    ├── cmd/doink/            # main: flags, console mode, tray mode (tray_windows.go)
    └── internal/
        ├── savedvars/        # parses DOINK.lua: events + webhooks (bytes, Lua strings)
        ├── shot/             # reads the realtime grid out of a screenshot
        ├── state/            # posted seqs per character (state.json, gitignored)
        ├── discord/          # embed builders + webhook Pool (rate limits, 429s)
        ├── wowhead/          # item icon + tooltip lines for loot embeds, cached
        ├── config/           # loads config.toml (optional, every key optional)
        ├── discover/         # finds <WoW>/_*_/WTF/Account/*/SavedVariables/DOINK.lua
        ├── watcher/          # (mtime, size) polling with a settle poll
        ├── status/           # thread-safe state for the tooltip/notifications
        ├── runner/           # the watch-and-post loop (Process, Run, RunOnce)
        ├── updates/          # GitHub release check (check only, no install)
        ├── tray/             # Windows tray icon + menu + single instance (x/sys)
        ├── settings/         # settings window (WebView2, Windows) + config.toml editing
        └── autostart/        # "Start with Windows": HKCU\...\Run\DOINK
```

## Data contract (both halves depend on this — do not change casually)

The addon stores **each event as a JSON string** inside a Lua table. Parsing
Lua table syntax from the companion is painful; a Lua table of strings is
trivial (scan the quoted strings, JSON-decode each). One small Lua encoder
saves a whole Lua parser in the companion.

SavedVariables global: `DOINKDB`

```lua
DOINKDB = {
  version = 1,
  debug = false,
  webhooks = {                  -- optional; flat so the companion can parse it
    ["*"] = "https://discord.com/api/webhooks/...",              -- all chars
    ["Flano-Whatever"] = "https://discord.com/api/webhooks/...", -- override
  },
  chars = {
    ["Flano-Whatever"] = {
      seq = 42,                 -- last seq issued for this char
      config = { ... },         -- per-char config overrides
      kills = {                 -- first-kill memory: encounter id -> ts
        ["672"] = 1759300000,
      },
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
  "surname": "Wren",           // optional; Forever only
  "realm": "Whatever",
  "class": "WARRIOR",
  "level": 20,
  "type":  "level_up",
  "data":  { ... type-specific ... },
  "test":  true                 // optional; only present on /doink test events
}
```

Rules:
- Forever characters have surnames, and only the **full name** is unique.
  `char` stays the first name; `surname` is present when the client has one.
  Character keys everywhere (DB `chars`, `webhooks`, companion `state.json`)
  are `"<char> <surname>-<realm>"`, or `"<char>-<realm>"` without a surname.
  Source: `UnitName("player")` returns the surname as its 2nd value on Forever
  (verified; nil elsewhere). Since v0.3.0; both halves migrate v0.2.0's
  first-name keys (the addon also stamps `surname` into queued events).
- Webhook for a character's events: `webhooks["Name-Realm"]`, else
  `webhooks["*"]`, else the companion's `config.toml`. Set in game with
  `/doink webhook [here] <url>`; never print a full URL in chat.
- `loot.vendor_value` is the whole stack (`sellPrice * qty`), in copper.
- `death.killer` comes from the client's death recap
  (`DeathRecap_GetEvents()`, present in Forever): the attacker of the newest
  hit. For an environmental death `killer` is `null` and
  `death.environment` holds the recap's `environmentalType` upper-cased
  (`FALLING`, `DROWNING`, `FATIGUE`, `FIRE`, `LAVA`, `SLIME`); both chat
  and Discord word those themselves ("Forgot I couldn't fly."). Both `null`
  when the recap is empty (the notifier retries once after 0.5 s) or the
  API is absent. The combat log is protected, so there is no other source.
- `test` is present (and `true`) only for events from `/doink test`. The
  companion should still post them, but label them as tests.
- `seq` is per-character, monotonic, never reused. The companion dedupes on it.
- `ts` is `time()` (Unix seconds, server-ish time).
- Ring buffer: when `#events > 500`, drop from the front.
- `data` shapes per type:
  - `level_up`:  `{"level": 20}`
  - `loot`:      `{"item_id": 123, "link": "|cnIQ3:|Hitem:123:...|h[Name]|h|r", "name": "...", "quality": 3, "qty": 1, "vendor_value": 1234, "boe": true|false|null, "gear": true|false|null}`
  - `death`:     `{"zone": "Westfall", "subzone": "...", "killer": "Defias Pillager"|null, "environment": "FALLING"|null}`
  - `quest`:     `{"quest_id": 123, "title": "...", "xp": 1200}`
  - `boss_kill`: `{"encounter_id": 123, "name": "...", "instance": "...", "difficulty": 1, "success": true, "group_size": 5, "first_kill": true|false, "group": ["Flano Wren (Warrior 23)", ...]|null}`
  - `skill_up`:  `{"skill": "Blacksmithing", "rank": 150, "max_rank": 150}`
  - `mount`:     `{"name": "Gray Wolf", "item_id": 5656|null, "spell_id": 6777|null, "link": "|Hitem..."|null, "bought": true|false}`
  - `duel`:      `{"opponent": "Grakkor Highmountain", "won": true|false, "fled": true|false, "makgora": true|false}`
  - `battleground`: `{"name": "Warsong Gulch", "won": true|false}`
- `loot.boe` is `bindType == 2` (Bind on Equip; `null` when the bind type is
  unknown). `loot.gear` is item class 2 (weapon) or 4 (armor); `null` when
  the class is unknown.
- `boss_kill.first_kill` is true the first time this character kills that
  encounter (remembered in `chars[key].kills`, keyed by the encounter id as
  a string). `boss_kill.group` lists the party members other than the
  player (at most 4) as `"Name Surname (Class Level)"`; `null` in a raid,
  when solo, or when any name is secret.

## Notifiers (v1)

| Type        | Trigger event              | Default filter                                   |
|-------------|----------------------------|--------------------------------------------------|
| `level_up`  | `PLAYER_LEVEL_UP`          | every level (option: milestones 10/20/.../60)    |
| `loot`      | `CHAT_MSG_LOOT`            | quality >= 3 (rare/blue) OR vendor value >= N    |
| `death`     | `PLAYER_DEAD`              | always                                           |
| `quest`     | `QUEST_TURNED_IN`          | always                                           |
| `boss_kill` | `ENCOUNTER_END`            | success only                                     |
| `skill_up`  | `CHAT_MSG_SKILL`           | milestones only (75/150/225/300)                 |
| `mount`     | `NEW_MOUNT_ADDED`, `CHAT_MSG_LOOT` | always (one per 120 s)                   |
| `duel`      | `CHAT_MSG_SYSTEM`          | wins only (`wins_only`)                          |
| `battleground` | `UPDATE_BATTLEFIELD_STATUS`, `PVP_MATCH_COMPLETE`, `PLAYER_ENTERING_WORLD` | wins only (`wins_only`) |

Loot: `rares_boe_only` (default true) drops rares (quality 3) only when they
are known not to be Bind on Equip (an unknown bind type, `boe = null`,
still posts); legendaries (quality 5) always post when they are gear
and never when they aren't. Boss kills: `first_kill_only` (default false).

Deferred to v1.1 pending beta findings: achievements (vanilla had none; Forever
may add them), rare mob kills (combat log `UNIT_DIED` + classification).

## Announcements (in-game chat)

`Announce.lua` is registered in `ns.Transports` like the screenshot transport: Core
calls `Send(json, envelope)` for every emitted event and never knows what the
transport does with it. Rules live in `ns.Defaults.announce` (per-character
overrides via `ns:GetOption("announce", key)`; allowed string values in
`ns.Choices.announce`): `channel` guild|officer|party|raid|off, `level_up`
milestones|all|off, `loot` minimum quality any|uncommon|rare|epic|legendary|off,
`skill_up` max|milestones|all|off, `death`/`quest`/`boss_kill`/`mount`/`duel`/`battleground`
booleans (the last three default on).
Defaults are deliberately stricter than the Discord queue's (milestones,
epic+, deaths, boss kills; quests and skill-ups off): guild chat is shared.

- Wording is first person after the chat prefix ("Ding! Level 20.", "Looted
  <link>!", "Ragnaros down! (Molten Core, 40 players)", "Died in Westfall.").
  Item links are the raw `|Hitem` link from the loot event and render as
  links in chat. Quests are plain text (a hand-built quest link could be
  malformed).
- **Test events never reach the guild**: anything with `test = true`, and
  `/doink announce test`, is whispered to the player's **full name**
  (`UnitName` gives the first name only on Forever; whispering "Flano" fails
  with "No player named 'Flano'").
- Rate limit: one line per 2 s via a `C_Timer.After` chain, max 8 per minute,
  dropped lines are printed to the player. 255-byte chat limit enforced.
- Verified in beta (2026-10-02): `SendChatMessage` **works from a timer**, i.e.
  without a hardware event: WHISPER (server answered) and a real PARTY
  announcement from a live event both delivered. GUILD is the same
  restriction class in retail; not yet seen live (no guild on the test char). SAY/YELL/CHANNEL are hardware-gated
  for addons and are not offered. `C_Club` and `C_Club.SendMessage` exist in
  Forever but are untested (no community to test with); communities are a
  later option.

## Settings page (Options.lua)

Forever has the modern `Settings` API (verified 2026-10-02:
`RegisterVerticalLayoutCategory`, `RegisterProxySetting`, `CreateDropdown`,
`CreateSlider` present; legacy `InterfaceOptions_AddCategory` absent). The
page is registered at `PLAYER_LOGIN` (values are per character) and every
control is a **proxy setting** onto `ns:GetOption`/`ns:SetOption` (or
`DOINKDB.realtime` for the account-wide realtime settings), so the page and
the slash commands can never disagree. `/doink config` opens it. The
7-argument `RegisterProxySetting(category, variable, type, name, default,
get, set)` form is what the client accepts. No text input exists in that
API, so the webhook stays with `/doink webhook` and the companion window
(decided). Controls are built from spec tables, not positional args: a
function returning two values is truncated to one when it isn't the last
argument, which once turned a tooltip into a setter.

## Screenshot transport contract (realtime, since v0.14)

Realtime = Discord within seconds instead of at `/reload`. The addon draws
a thin black-and-white strip along the top edge for one frame and calls
`Screenshot()`;
the companion finds the file in `<flavor>/Screenshots`, decodes the grid,
posts the event and deletes the file. Verified in retail (2026-11-02, see
beta facts): `Screenshot()` works from a timer and in combat, and the
screenshot contains addon frames pixel-exact. Ideas from
wow-character-tracker (GPL-3): no code taken.

History: v0.6–v0.11 used a pixel strip read off the window by the Python
companion (BitBlt, foreground only, Windows 11 corner rounding; see beta
facts). The Go port dropped the reader; v0.14 deleted the strip. Code:
`git show v0.13.0:addon/DOINK/Transports/Pixel.lua`,
`git show v0.11.7:companion/doink/pixel.py`.

`addon/DOINK/Transports/Screenshot.lua` encodes, `companion/internal/shot`
decodes. `addon/tests/fixtures/shot_levelup.txt` (first line `payload
<json>`, then one line of 0/1 per row) is the Lua encoder's output; the Lua
test asserts it and the Go test checks `shot.Rows` against it, so the two
can't drift. Regenerate with `DOINK_WRITE_FIXTURE=1 luajit
tests/test_addon.lua`.

- **Grid**: a strip along the top edge, `COLS` = screen width / 2 − 1
  blocks (256..1200; 959 at 1920 px), `B` = 2 physical px per block (the
  smallest that survives the game's JPEG; 1 px doesn't). A box-shaped
  256-wide grid was tried first and was far more noticeable than the old
  9 px pixel strip; a wide strip keeps a typical event to 4 rows = 8 px.
  Row 0: sync `1010101010110011`, then MSB-first `version(4)=2, flags(4)`
  (bit 0 = hello), `cols(16)`, `rows(8)` = R data rows (1..32),
  `payload_len(16)`, `payload_crc16(16)` (CRC-16/CCITT-FALSE, 0x1021,
  init 0xFFFF), `header_crc8(8)` (0x07, init 0, over the 8 bytes before
  it); 88 blocks, rest 0. Rows 1..R: payload bits row-major, `cols` per
  row, padding 0. Max payload 1024 bytes; longer messages aren't sent
  (the file path posts them). One message per screenshot. 1 = white,
  0 = black, on a black backing one block bigger right and bottom. The
  decoder's block-size sweep only helps near-exact sizes; the grid is
  pixel-perfect in practice (verified).
- **Frame**: child of `UIParent` (WorldFrame children draw under the UI),
  strata `TOOLTIP`, level 10000, mouse off, pixel-perfect via
  `SetIgnoreParentScale(true)` + `SetScale(768/physicalHeight)`,
  `TOPLEFT` at (0,0): the screenshot is the game's own render, so no
  corner inset. Texture rows are created as messages need them. A separate
  parentless driver frame owns `OnUpdate` and the events.
- **Message** = the event's JSON exactly as queued, or a hello
  `{"type":"hello","addon":..,"char":..,"surname":..,"realm":..,"test":true}`
  (flag bit 0) from `/doink realtime test` only; no hello at login (each
  costs a screenshot and a "Screen captured").
- **State machine**: queue max 8 (drop the oldest not being shot). idle →
  (queue non-empty, ≥ 1.1 s since the last shot because file names have 1 s
  resolution, `UIParent:IsVisible()`) paint + show + `Screenshot()` in
  the same `OnUpdate`, hide on the next: the client captures the frame it
  draws after the call (retail 2026-11-02: hidden in the same handler →
  not in the file; hidden one frame later → in it), so the grid is on
  screen for one frame. `SCREENSHOT_SUCCEEDED` pops; `SCREENSHOT_FAILED`
  retries once; no answer in 3 s moves on.
- **Format swap**: `screenshotFormat` other than jpeg/png → jpeg, and
  `screenshotQuality` < 3 → 3, for the shot only; the old values wait in
  `DOINKDB.realtime.restore` and are put back after the shot or at the
  next login (a `/reload` mid-shot).
- **Blocked**: `ADDON_ACTION_BLOCKED`/`ADDON_ACTION_FORBIDDEN` naming DOINK
  turns realtime off with one chat line. The popup can't be caught with
  `pcall`, so this is the only guard if Forever or a boss encounter
  blocks `Screenshot()`.
- **Companion** (`runner/shots.go`, on `Run`'s goroutine so it never races
  `Process`): each poll, for each `<flavor>/Screenshots` derived from the
  watched SavedVariables paths, files named
  `WoWScrnShot_\d{6}_\d{6}(_\d+)?.(jpg|jpeg|png)`. Each file is decoded once
  per run per (mtime, size); on the first scan files older than 14 days
  aren't opened; files under 500 ms old wait; unreadable images under
  10 s old are retried. No grid → left alone forever. Hello → log + tray
  notice "Realtime works for <name>", file deleted. Event → if not
  `IsPending` delete; else `PostEvents(..., MaxBacklog)` and delete on
  success; on failure keep the file (the SavedVariables path posts it
  later). Dry run deletes nothing. Webhooks: the in-game ones from that
  flavor's SavedVariables files, then config.
- **Dedupe**: `state.json`'s `missing` (seqs below `last_seen` not yet
  posted) lets realtime posts run ahead of the file; the file pass posts
  `seq > last_seen or seq in missing`. Unchanged from the pixel era.
- **Opt-in in game only**: `DOINKDB.realtime.enabled` (default false); the
  companion's `realtime` defaults to true (it only acts on DOINK's own
  screenshots), `realtime = false` turns reading off.
- `doink decode <image>...` prints what a screenshot's grid says; changes
  nothing.

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

- **Go** (module `github.com/ryan-flan/DOINK/companion`, `go 1.23`), via
  devbox: `devbox shell`, or `devbox run test` / `test:companion`. Go is
  not on the host PATH. Run: `cd companion && go run ./cmd/doink --dry-run
  --once --config <path>/config.toml` (pass `--config` in dev: `go run`
  builds into a temp dir, and the default is `config.toml` next to the
  executable). Tests: `go test ./...`; offline (network = `httptest`,
  files = `t.TempDir()`).
- **Dependencies:** `github.com/BurntSushi/toml`, `golang.org/x/sys` and
  (Windows only, for the settings window) `github.com/jchv/go-webview2`
  (MIT, pure Go, pulls in `jchv/go-winloader`), nothing else at runtime. `x/sys` is pinned to the newest release that
  still supports `go 1.23` (v0.35.0; later ones need Go 1.24+/1.26).
  Build-time only: `go-winres` via `go run` in `release.yml`, not in
  `go.mod`.
- Packages: `internal/{savedvars,state,discord,wowhead,config,discover,
  watcher,status,runner,updates,tray,autostart,settings}` and `cmd/doink`. `tray`
  and `autostart`'s registry code are `//go:build windows`; their tests run
  only on the Windows CI job, which also runs the whole suite.
- Packages that log without a `Deps` (`savedvars`, `config`, `watcher`,
  `updates`, `tray`) have a package `Log *slog.Logger`; nil means
  `slog.Default()`, which `cmd/doink` sets. Log messages are formatted
  strings, not attrs (one readable line, multi-line dry-run JSON intact).
- Config paths may be written Windows-style (`C:\...`): off Windows,
  `config.NativePath` maps them to `/mnt/c/...` if that exists (WSL), else
  into a Wine prefix's `drive_c` that has the path, else the `/mnt` form.
- Poll the SavedVariables file's `(mtime, size)` every `poll_interval` (2 s).
  No OS file-watch hooks. The watcher reports a change only once the
  signature has held still for one poll, so it never reads a half-written
  file. `config.toml` is polled the same way and reloaded live
  (`runner.Run`'s `reload`); a bad edit logs a warning and keeps the old
  settings; a changed path list rebuilds the watchers.
- On change: parse → per character, events still pending (`seq >
  last_seen` or in `missing`) → POST in order → persist state after each
  message. Never re-post on restart.
- Parsing works on **bytes**: find every `["events"] = {` block, read Lua
  short strings (all Lua escapes incl. `\ddd`), `ToValidUTF8`, JSON-decode
  each with `UseNumber` (seq must be an integer; data numbers become
  float64). A bad entry is skipped with a warning; an unexpected token
  aborts the rest of that block.
- Discord: one embed per event, colour by class (loot uses item-quality
  colour), Wowhead links for items and quests
  (`https://www.wowhead.com/forever/item=<id>`, `/quest=<id>`; verified),
  footer `Name Surname-Realm`, `[TEST] ` titles for test events. Up to 10
  embeds per message (Discord's limit); one `Pool` entry per webhook URL,
  each with at least 2 s between messages (longer if
  `X-RateLimit-Remaining` is 0); 429 `retry_after` is respected (1 s if the
  body isn't JSON), 5 attempts. Unknown types fall back to a JSON
  description so nothing is lost.
- Loot embeds are enriched from Wowhead (`internal/wowhead`, since v0.10.0):
  `https://nether.wowhead.com/forever/tooltip/item/<id>?locale=0` returns
  `{name, quality, icon, tooltip (HTML), spells}` (404 for unknown ids);
  the icon is `https://wow.zamimg.com/images/wow/icons/large/<icon>.jpg`.
  The tooltip HTML becomes description lines (name, sell price and stack
  size dropped, max 12), the icon the thumbnail. One GET per item per
  session, misses cached too, 5 s timeout, never panics; `wowhead = false`
  disables it. The README's network statement names Wowhead; keep it
  accurate.
- Zero config is the goal: `config.toml` is optional and so is every key
  (`wow_dir`, `savedvariables_path`, `webhook_url`, `dry_run`,
  `poll_interval`, `max_backlog`, `wowhead`, `update_check`,
  `update_notify`, `realtime`; `[notifiers]` is logged and ignored).
  Without a path, `discover` scans the standard WoW folders on every drive
  (Windows), every `/mnt/<letter>` and every Wine/Proton prefix's
  `drive_c` (`~/Games/*`, `~/.wine`, Lutris prefixes incl. Flatpak, Steam
  `compatdata/*/pfx`), keeping flavors where `Interface/AddOns/DOINK`
  exists, and watches every account's file. Notifier toggles are **not**
  companion config.
- `config.toml`, `state.json` and `doink.log` live next to the executable.
- Webhook precedence: in-game per-char > in-game `*` > `webhook_url` in
  config.toml. A character with no resolvable webhook logs an error and its
  events stay queued (state not advanced) until one is set.
- First time a character is seen, post at most `max_backlog` (default 10) of
  its queued events. If the newest seq in the file is below `file_seen`
  (the highest seq a previous file pass showed; SavedVariables wiped), treat
  the character as new. Never compare with `last_seen` for this: posts from
  another path (realtime) run ahead of the file until the next `/reload`,
  and a companion restart in that window once re-posted ten events
  (2026-10-02). `state.json` keeps the Python companion's format exactly.
- `doink test <type>... [url]` (`all`, or the addon's type names, matched
  the same way) posts the addon's `/doink test` fixtures (`cmd/doink/
  testevents.go`; keep them in step with each notifier's `Test()`) as
  `Flano Wren-DOINK` to the given URL, else `webhook_url`, else the
  in-game `*` webhook. Wowhead-enriched, `--dry-run` aware, never touches
  `state.json`. For checking embeds without logging in.
- **Two modes.** Console: everywhere, and on Windows with `--once`,
  `--console`, `--dry-run`, `--test-webhook` or `test`; logs `15:04:05 LEVEL msg` to
  stderr (on Windows it attaches to the parent console: the exe is
  `-H windowsgui`). Tray (Windows default): logs to `doink.log` (1 MB, then
  `doink.log.1`), named mutex `Local\DOINK-companion` against a second copy,
  startup errors in a message box, "WoW not found" and "no webhook yet" are
  states shown in the tooltip/balloon, not fatal. The tray runs on the main
  goroutine (locked OS thread); the runner and update checker are
  goroutines; runner → tray goes through `Status` (`onChange` posts
  `WM_STATUS` to the tray window).
- Settings window (`internal/settings`, since v0.13; decided 2026-11-02,
  reversing v0.12's "no settings window"): tray menu → Settings. HTML/CSS
  page (`ui.html`, embedded, CSP blocks all network) in a WebView2 window
  via `go-webview2`, on **its own locked OS thread** with its own COM
  apartment and message loop: closing a go-webview2 window posts
  `WM_QUIT` to its thread, which must not be the tray's. One window at a
  time; Show from any goroutine; `Status` changes push `doinkLive` into it.
  Per-thread DPI awareness (per-monitor v2), dark title bar from
  `AppsUseLightTheme`. WebView2 data lives in
  `%LOCALAPPDATA%\DOINK\WebView2` (the README says so). No WebView2
  runtime → falls back to opening `config.toml` (3-line template if
  missing). It edits only `webhook_url` and `wow_dir`, by rewriting those
  lines in place (`settings.Set`: comments and other keys kept, result
  re-parsed before an atomic write). The logic (`Backend`: view, save,
  validate, test post) is platform-neutral and tested on Linux; only
  `window_windows.go` is Windows-only. The library's own `init` locks the
  main goroutine to the main thread and CoInitializes it; harmless.
- Tray code uses `golang.org/x/sys/windows` lazy DLL procs with structs
  laid out as Win32 expects (64-bit sizes are asserted in the Windows
  tests). One `syscall.NewCallback` window procedure for the process,
  dispatching by `hwnd`. Icons are embedded (`companion/assets`) and written
  to `%TEMP%\doink-*.ico` for `LoadImageW`.
- Realtime: see "Screenshot transport contract". The companion never
  captures the screen.
- Updates (`internal/updates`): one GET to the GitHub releases API 30 s
  after start and then daily (1 h retry after a failure; `update_check =
  false` disables; the tray's "Check for updates" always works). A newer
  version swaps the tray icon to `doink-update.ico` (orange dot) and the
  menu offers to open the download page. **Check only**: no download, no
  self-install (that's a later milestone). Notifications only with
  `update_notify = true`, once per version per session. Console mode
  doesn't check. Lesson kept from the Python self-updater, for when Go
  gets one: launch the hand-off script with `CREATE_NO_WINDOW`, never
  `DETACHED_PROCESS` (a detached cmd.exe from a windowless exe never ran,
  2026-10-03), and sleep with `ping`, not `timeout`.
- Trust is a feature: autostart is off by default and writes exactly one
  per-user Run value; a named mutex stops a second copy (double posts); the
  exe has a version resource and icon; releases ship a `.sha256`. Keep the
  README's "What doink.exe does" section accurate when behaviour changes.
- Builds: one static exe (`CGO_ENABLED=0`, `-trimpath -s -w`, `-H
  windowsgui` on Windows), Linux amd64 too. The release workflow
  smoke-tests both with `--once` on a dry-run config. The Windows zip keeps
  the Python era's names and layout (`DOINK-<tag>.zip`,
  `<name>/Companion/doink.exe`, `.sha256` as `"<hex>  <name>.zip"` with no
  newline): v0.11.x self-updaters on existing installs depend on them.

## Dev setup (reference)

- Personal machine details (OS, paths, who tests what) live in
  `CLAUDE.local.md`, which is gitignored. Keep this file generic.
- Toolchain via devbox (`devbox.json`: Go, LuaJIT, gh): `devbox shell`, or
  `devbox run test` / `test:addon` / `test:companion` / `dry-run`. Don't
  assume Go is on the host PATH.
- Addon in game: put `addon/DOINK` at
  `<WoW>/<flavor>/Interface/AddOns/DOINK`, as a copy or a link (Windows:
  `mklink /J`; Linux/Wine: `ln -s`, untested under Wine).
- In-game helpers: BugSack + BugGrabber (errors), DevTool (`/dev`), built-in
  `/etrace`, `/dump`, `/fstack`, `/console scriptErrors 1`.
- SavedVariables path:
  `<WoW>\<flavor>\WTF\Account\<ACCOUNT>\SavedVariables\DOINK.lua`
- Remote: https://github.com/ryan-flan/DOINK (public), default branch `main`.
- CurseForge project ID: 1721438 (repo variable `CURSEFORGE_PROJECT_ID`).
  Wago Addons project id: repo variable `WAGO_PROJECT_ID` (8-character id
  from the Wago developer dashboard).

## Releases

1. Bump `## Version` in `addon/DOINK/DOINK.toc`. The companion's version
   comes from the tag (`-ldflags -X main.version=X.Y.Z` in
   `release.yml`; `dev` in local builds). Commit as
   `build: release vX.Y.Z`.
2. `git tag -a vX.Y.Z -m "DOINK vX.Y.Z"` and `git push origin vX.Y.Z`.
3. `release.yml` runs four jobs:
   - `build` (Windows): tests, `go-winres` (icon + version info), builds
     `doink.exe`, smoke-tests it, packages `DOINK-vX.Y.Z.zip` (AddOns/,
     Companion/, README, LICENSE) + `.sha256`.
   - `linux` (Ubuntu): builds `doink` (linux/amd64), smoke-tests it,
     packages `DOINK-vX.Y.Z-linux-amd64.tar.gz` (doink,
     config.example.toml, README, LICENSE) + `.sha256`.
   - `release` (tags only, after both): publishes those four files as a
     GitHub release. Notes are generated from commits. Manual runs stop
     after the artifacts (`DOINK-dev.zip`, `DOINK-dev-linux-amd64.tar.gz`).
   - `addon` (BigWigs packager v2, needs ≥ 2.6.0 for Forever): builds the
     **addon-only** zip `DOINK-vX.Y.Z-forever.zip` from `.pkgmeta`
     (`move-folders` lifts `addon/DOINK` to the zip root; ignore rules run
     first, so never ignore `addon/` itself). Interface 16xxx → game type
     `forever`, game version 1.60.1. On tags it uploads to each store whose
     repo **variable** (project id) and **secret** (API key) are set, and
     skips the rest: CurseForge (`CURSEFORGE_PROJECT_ID`, `CF_API_KEY`),
     Wago Addons (`WAGO_PROJECT_ID`, `WAGO_API_TOKEN`; the packager sends
     game type `forever`), WoWInterface (`WOWI_PROJECT_ID`,
     `WOWI_API_TOKEN`; wired but unused, no Forever category seen there).
     Manual runs pass `-d` and never upload. The store zip never contains
     `doink.exe`; the store pages link to GitHub Releases for the
     companion. **README.md is the store description**: Wago pulls the
     GitHub README automatically and the CurseForge page is pasted from it,
     so it stays features-first, store-neutral, with the "how it works" and
     AI-use sections at the end (`docs/store-description.md` was folded into
     it). Logo: `docs/logo-400.png`.
     Secrets go in through the GitHub web UI, never via chat; `gh secret
     set` once stored an empty value.

Semver: breaking data-contract changes bump the minor version while < 1.0.

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

(The test character is called "Flano Wren" throughout this repo, tests and
fixture included; that's a stand-in for the real name, by decision.)

- [x] Flavor folder name: `_forever_` for the live client (`.flavor.info`:
  `wowf`; seen in a live install 2026-11-02). The beta was
  `_classic_beta_` (`wow_classic_beta`). Windows default install:
  `C:\Program Files (x86)\World of Warcraft\`.
- [x] TOC `## Interface:` version: `16001` (client `1.60.1 70124`, Sep 29
  2026); still current 2026-11-01.
- [x] **Retail 12.1.5 blocks GUILD chat from addon code without a key
  press** (2026-11-02): `SendChatMessage(..., "GUILD")` from a
  `C_Timer.After` callback → "Interface action failed because of an
  addon"; the same call straight from `/run` (the Enter key press) posts,
  and a WHISPER from a timer posts. So on retail, DOINK's guild
  announcements (sent from game events and a timer queue) can't go out
  by themselves. Forever: untested, see the launch checklist.
- [x] **Blizzard's Discord guild-chat link** (Forever beta; from
  Blizzard/Discord posts, the kami-labs guide and Blizzard forums, not
  tried by us). Server-side relay of guild chat ↔ one Discord channel.
  Player: Esc → Options → Social → "Enable Discord Functionality" (links
  Battle.net ↔ Discord). Guild leader/officer who is also a Discord admin:
  J → Guild Settings → Guild Control → Discord Settings → server, channel,
  Link (needs Manage Channels, View Channel, Send Messages). Display name
  options: Default / Last Online Character Name / Discord Global Name.
  Officers route Discord into `/guild` or a separate `/discord` chat.
  Unlinked members' lines don't reach Discord ("… has not yet linked
  their Discord account"). Sept-2026 bug: Horde/PvP guilds' Link button
  did nothing. Because `Announce.lua` uses `SendChatMessage(…, "GUILD")`,
  DOINK's lines should show in the linked channel for linked players; the
  README says so ("Already linking your guild to Discord?").
  - [x] Tried on retail 12.1.5 (2026-11-02, a linked guild): a guild line
    sent by addon code (from `/run`, i.e. with a key press) reached the
    Discord channel. Item links arrive as plain text (`Looted
    [Hearthstone]!`: no colour, no link); Discord shows the player's
    Discord name and avatar with a game-controller badge. A message typed
    in Discord arrives in game as a normal `CHAT_MSG_GUILD` whose sender
    is the Discord name (`flano`: lowercase, no realm); the chat frame
    adds a Discord icon. So addons can read Discord messages, but on
    retail can't answer them automatically (GUILD needs a key press).
  - [ ] Which `CHAT_MSG_GUILD` argument marks a Discord-origin message
    (needed before acting on Discord messages).
- [ ] **Launch-day checklist** (2026-11-04; nothing here is verified yet):
  - [ ] **First: can an addon post to GUILD without a key press?**
    `/run C_Timer.After(1,function() SendChatMessage("test","GUILD") end)`.
    Retail 12.1.5 blocks it (see below); if Forever does too, every
    automatic guild announcement fails with "Interface action failed
    because of an addon", and `Announce.lua` must change before anyone
    levels (announce on a click or key press instead).
  - [ ] DOINK guild lines appear in the linked Discord channel; how
    `[Item]` links render there.
  - [ ] `/dump ChatTypeInfo` and `/etrace` while someone types from
    Discord: is there a `DISCORD`-ish chat type? If `SendChatMessage`
    accepts it, add `discord` to `ns.Choices.announce.channel`.
  - [ ] `NEW_MOUNT_ADDED` fires on Forever; `C_MountJournal` present?
  - [ ] `GetItemInfo`'s 14th value (bindType) is present;
    `GetItemInfoInstant` returns class/subclass ids (weapon 2, armor 4;
    mounts 15/5).
  - [ ] `LOOT_ITEM_PUSHED_SELF` exists ("You receive item:"), so a
    vendor-bought mount item is seen.
  - [ ] Real `DUEL_WINNER_*` text, and the Mak'gora global names: does
    the `_G` scan in `Duel.lua` pick up a Mak'gora challenge or result
    string (and nothing else)?
  - [ ] `GetBattlefieldWinner` exists; `PVP_MATCH_COMPLETE` fires.
  - [ ] Party units' `UnitName("party1")` returns the surname as the
    second value, as `UnitName("player")` does (boss-kill group list).
  - [ ] Python v0.11.7 self-update to the Go v0.12.0 lands `doink.exe`
    and removes `_internal`.
  - [ ] Retest the chat/combat log flush timing (see below).
  - [ ] `C_Timer.After(2, function() Screenshot() end)` from `/run` in
    Forever: `SCREENSHOT_SUCCEEDED`, no blocked popup (passes in
    retail, see below).
  - [ ] Realtime end to end in Forever: `/doink realtime on`, a real
    event posts within ~3 s and its screenshot is deleted; `doink decode`
    on a kept copy reads it. Commit the grid's bounding box cropped from a
    real Forever screenshot as `companion/internal/shot/testdata/` for a
    regression test (crop only the grid: nothing personal).
  - [ ] Realtime during a dungeon boss fight: shot taken, or blocked and
    realtime turned itself off.
- [x] **Addons can take screenshots** (retail 11.x client, 2026-11-02;
  **not yet Forever**, which shares the engine). `Screenshot()` called
  from a `C_Timer.After` callback, i.e. with no hardware event, fires
  `SCREENSHOT_SUCCEEDED` and writes
  `_retail_/Screenshots/WoWScrnShot_MMDDYY_HHMMSS.jpg` (750–815 KB at
  the default JPEG settings), out of combat and while attacking a
  training dummy. `C_CVar.SetCVar("screenshotQuality", "3")` before the
  call also works; the settings read `jpeg 3` beforehand (the default,
  or left over from an earlier test that day), and all three files
  were the same size, so the swap may be unnecessary. No blocked-action
  popup, and no "Screen captured" line in chat. The game shows its
  standard "Screen captured" message in the centre of the screen (fine,
  decided); no visible stutter. The screenshot includes addon frames
  exactly: a 64×64 `UIParent` child (strata `TOOLTIP`,
  `SetIgnoreParentScale(true)`, `SetScale(768/physicalHeight)`,
  `TOPLEFT`) came out as magenta pixels (0,0)–(63,63) with a sharp edge
  in a 2554×1404 JPEG. Unchecked: during a dungeon boss encounter.
  - [x] v0.14.0 end to end in retail (2026-11-02, a dev copy with a
    `DOINK_Mainline.toc`, Interface 120105): `/doink realtime test`,
    `test levelup` and `test loot` each took a screenshot with no errors;
    `doink decode` read all three real JPEGs (hello, 135-byte level-up,
    344-byte loot), and a `--dry-run --once` companion run logged the
    hello, printed both embeds and would have deleted all three files. This is the basis for the planned `Screenshot()`
  channel (spec "Later"). Tip: run `/run` lines one at a time; pasting
  several at once ran none of them, silently.
- [x] Achievements present? API exists (`GetTotalAchievementPoints()` → `0`),
  not enabled in beta yet. Exploration achievements have been shown. v1.1.
- [x] **`COMBAT_LOG_EVENT_UNFILTERED` is protected for addons** (as in the
  current retail engine). Registering it at load pops "DOINK has been blocked
  from an action only available to the Blizzard UI". It doesn't throw, so
  `pcall` can't catch it. Never register it; `addon/tests` guards this.
  Combat-log data must come from the companion reading `WoWCombatLog.txt`.
- [x] **Log files are written late, not never** (tested 2026-10-01):
  `Logs\WoWChatLog.txt` stayed 0 bytes through a self-whisper and after
  `/chatlog` off, then filled on exit about an hour later (1.9 KB; the
  whisper is in it, both directions). `Logs\WoWCombatLog-MMDDYY_HHMMSS.txt`
  stayed empty through a fight and filled ~4 minutes after it (50 KB). So
  neither file is a realtime transport; the pixel bridge is. The combat log
  header reads `COMBAT_LOG_VERSION,22,...,BUILD_VERSION,1.60.1,PROJECT_ID,18`
  (Forever's project id is 18). Retest the flush timing at launch.
- [x] **Surnames.** `UnitName("player")` and `UnitFullName("player")` both
  return `"Flano", "Wren"` (surname where other clients put the realm);
  `GetUnitName("player", true)` returns `"Flano Wren"`. Related APIs exist
  but are unverified and unused: `C_PlayerInfo.ShouldDisplaySurname`,
  `C_NameUtil.ReplaceSurnameSeparatorWithLinkSeparator`.
- [x] **Window class** of the Forever beta client (`WowB.exe`) is
  `waApplication Window` (title "World of Warcraft"), not retail's
  `GxWindowClass`. `pixel.find_wow_window` tries both.
- [x] `ENCOUNTER_END` fires in Forever dungeons (2026-10-02, Shadowfang
  Keep): `ENCOUNTER_START 2748 "Rethilgore" 1 5` on the pull, then
  `ENCOUNTER_END 2748 "Rethilgore" 1 5 1 <table>` on the kill. The trailing
  table (printed as `table: 00000258C1CC77F0`) is new; contents unknown and
  unused. The boss-kill announcement and Discord embed both arrived.
  `BOSS_KILL` was registered too but didn't show in the screenshot.
- [x] Another player's stack loot line: `Grakkor Highmountain receives
  loot: [Medium Leather]x2` (no space before `x2`), consistent with
  `LOOT_ITEM_SELF_MULTIPLE`; a self-loot stack is still unseen.
- [x] `QUEST_TURNED_IN` fires with questID/xp on a real turn-in, and the
  title resolves (quest 818 "A Solvent Spirit", 625 xp).
  `C_QuestLog.GetQuestInfo` returns `nil` for quests not in the log.
- [x] During `PLAYER_LEVEL_UP`, `UnitLevel("player")` still returns the
  **old** level. Core tracks `ns.knownLevel` from the event for the envelope.
- [x] `SKILL_RANK_UP` = `Your skill in %s has increased to %d.` (keeps its
  period, unlike the loot strings).
- [x] **`GetNumSkillLines` does not exist in Forever** (first two user bug
  reports, 2026-10-03: `SkillUp.lua:10: attempt to call a nil value` on a
  milestone skill-up). The modern engine dropped the skills panel API.
  The notifier now tries `GetProfessions`/`GetProfessionInfo` instead and
  settles for `max_rank = null` otherwise; weapon skills ("Bows") have no
  max-rank source at all.
  - [x] Verified 2026-10-03: `GetNumSkillLines` is `nil`; `GetProfessions`
    and `GetProfessionInfo` are functions, and `GetProfessionInfo(index)`
    returns `"Mining", 136248, 12, 75, 2, 21, 186, 0, -1, 0, "Mining"`
    (name, icon fileID, skillLevel, maxSkillLevel, numAbilities,
    spellOffset, skillLine, skillModifier, specializationIndex,
    specializationOffset, name again). The notifier uses the 4th value.
- [x] `PLAYER_DEAD` does **not** re-fire on `/reload`: three real deaths with
  a reload in between produced exactly three events (2026-10-02). Deaths
  post via realtime within ~1 s.
- [x] **Secret values exist in Forever** (user report, 2026-10-03, on
  entering a dungeon encounter): `Core.lua:38: invalid value (secret) at
  index 1 in table for 'concat'`, an 18-argument chat event whose text,
  sender and target were `<secret string>`. This is Midnight's encounter
  restriction: during a boss fight the client hands addons arguments they
  cannot read; `tostring()` of one is itself secret and `table.concat`
  throws. `issecretvalue(v)` identifies them (`ns.IsSecret`). The debug
  formatter ran for every event even with debug off and took dispatch
  down before any notifier ran; now it only runs with debug on and is
  pcall-guarded, and the chat-reading notifiers (loot, skill-up) skip
  secret messages. Consequence: **loot and skill-ups during a boss
  encounter aren't detected**; `ENCOUNTER_END` itself is fine (boss kills
  from that dungeon run arrived).
- [x] **`PLAYER_DEAD` can fire twice for one death** (a user's saved file,
  2026-10-03: two death events one second apart, same killer, on a
  hunter who says he did not feign death; the author's warrior has never
  seen it). Cause unknown; the pet dying with its owner is a guess.
  The notifier drops a second `PLAYER_DEAD` within 10 s of the last and
  ignores it while `UnitIsFeignDeath` is true, whatever the cause.
  - [ ] If it recurs on v0.11.6+, get `/doink debug` output from a death:
    it prints every `PLAYER_DEAD` the addon sees, with the time.
- [x] **Death recap exists** (2026-10-02): `DeathRecap_HasEvents()`,
  `DeathRecap_GetEvents()` (no id needed for the latest death) and
  `GetDeathRecapLink` are functions; `GetDeathRecap` is not. After a real
  death the events table held ten entries, each with `sourceName`
  ("Clattering Scorpid"), `amount`, and `spellName` on the poison tick;
  `environmentalType` was nil for all of them. `C_DeathInfo` only has corpse
  and graveyard helpers (`GetCorpseMapPosition`, `GetDeathReleasePosition`,
  `GetSelfResurrectOptions`, `GetGraveyardsForMap`, `UseSelfResurrectOption`).
  - [ ] Whether entries carry `timestamp` and which index is the killing
    blow: unverified; the notifier takes the newest by timestamp, else the
    last entry.
  - [x] The recap is already filled when `PLAYER_DEAD` fires: a real death
    named the scorpid at once, no retry (the 0.5 s retry stays as a guard).
  - [ ] Environmental `environmentalType` values unseen; retail uses
    Falling/Drowning/Fatigue/Fire/Lava/Slime, matched case-insensitively.
- [x] **Windows 11 rounded corners eat the strip's corner block**
  (2026-10-02, the real cause of "real deaths never post in realtime").
  Band dumps from v0.9.2 showed a perfect 0/255 strip, header decoded,
  payload CRC failing; the payload read as JSON with exactly one bit
  wrong: the last block of each full-width chunk (`"Scuttle Boast"`,
  `"ts":17908…`). The GDI capture of the window's bottom-right corner
  shows the DWM corner rounding (blend values 195/158/74/42/13 over the
  last ~6 px of the bottom rows); the game's own screenshot has a square
  corner. So that block reads as the desktop behind the window, and any
  chunk whose last byte is odd fails deterministically; earlier events
  passed by luck of content, which is why it looked tied to real deaths
  (killer/environment text) versus test deaths. Fix: the strip sits 12 px
  in from the side edge (`EDGE_INSET`), still flush with the top/bottom
  edge. Two earlier theories left useful hardening in `pixel.py`: local
  thresholding of payload rows, and near-integer fractional block widths
  (2.95–3.05 px failed outright before: 0.45 snap tolerance, central-half
  majority sampling, CRC-verified grid sweep). One unexplained reading
  remains: at 19:11 the sync was found 6 px left of the alive position
  with header CRC failures, possibly a transient death animation; the
  sweep handles that case now.
  - [x] Confirmed: a real death on v0.9.3 posted via realtime in ~1 s
    (2026-10-02 20:00).
- [x] **Communities parked**: `C_Club` exists but Battle.net features are
  limited in the beta (single US server), so nothing to test against.
- [x] `CHAT_MSG_LOOT` self-loot message format sample (raw, with link codes):
  `You receive loot: |cnIQ1:|Hitem:769::::::::8:1491::::::::::|h[Chunk of Boar Meat]|h|r`
  - Links use the **named-colour** form `|cnIQ<quality>:`, not `|cffRRGGBB`.
    Never parse quality from the colour hex; use `C_Item.GetItemInfo`, with
    `IQ<n>` as a fallback while item info is uncached. Item ID: `|Hitem:(%d+)`.
  - [ ] Stack sample (`...|h|rx2`) not yet seen live; inferred from template.
  - [x] Templates: `LOOT_ITEM_SELF` = `You receive loot: %s`,
    `LOOT_ITEM_SELF_MULTIPLE` = `You receive loot: %sx%d`. **No trailing
    period** (vanilla has one). Build match patterns from these globals,
    never hardcode the English.
  - Tip: in the chat box `||` is typed as a literal `|`; to show raw link
    codes use `msg:gsub("\124","\124\124")`.
- [x] SavedVariables string format: events are written as double-quoted Lua
  strings with `\"` escapes, one per line:
  `"{\"char\":\"Flano\",...,\"type\":\"level_up\"}",`

## Milestones

- **M1 — Skeleton. ✅ Done.** TOC (all files listed), Core, Json, Defaults, LevelUp
  notifier, all slash commands. Done when `/doink test levelup` → `/reload`
  produces a `level_up` JSON string in `DOINK.lua`.
- **M2 — First post. ✅ Done.** Companion watcher + parser + discord + state, posting
  the level-up embed. Done when the end-to-end loop works.
- **M3 — Notifiers. ✅ Done** (all fixtures post end to end; real-event
  checks still open in beta facts). Loot, Death, Quest, BossKill, SkillUp following the
  LevelUp pattern. Each with a `/doink test` fixture.
- **M4 — Polish. ✅ Done** (released as v0.2.0). Config via slash commands, README, PyInstaller build
  (built on GitHub Actions `windows-latest`; no local Windows Python).
  Config ownership (decided, implemented):
  - Notifier toggles and filters live **only in the addon** (it filters
    before emitting). Remove `[notifiers]` from the companion config.
  - Optional in-game webhook: `/doink webhook <url>` stored in `DOINKDB`,
    overridable per character (e.g. one channel per alt). Falls back to
    `config.toml`. This is a data-contract change; update the contract
    section first. Trade-off: the URL then sits in plain text in
    `DOINK.lua`, which people share when asking for addon help.
  - SavedVariables path stays companion-side (it must find the file before
    it can read anything) but should be auto-discovered from the usual WoW
    install locations so it rarely needs setting.
  - `dry_run`, `poll_interval`, `max_backlog` stay companion-side.
- **Post-M4 (done):** v0.3.0 surnames; v0.4.0 tray app, Start with Windows,
  MIT license; v0.5.0 settings window (webhook + test message, WoW folder
  picker, recent posts).
- **v0.6.0:** realtime via the pixel bridge (experimental, opt-in both
  sides; replaced by screenshots in v0.14). Not done: `PrintWindow` capture
  for an alt-tabbed WoW; `Screenshot()`-based capture as a fallback.
- **v0.7.0:** in-game chat announcements (`Announce.lua`); the addon is now
  useful without the companion, and the README/CurseForge page lead with
  that. See "Announcements".
- **v0.8.0:** settings page (`Options.lua`, native Settings API).
- **v0.9.0:** `death.killer` from the death recap, environmental death
  lines, `file_seen` wipe check. **v0.9.1–v0.9.3:** realtime deaths
  (decoder hardening; the real fix was the 12 px corner inset for Windows
  11 rounded corners). **v0.9.4–v0.9.5:** Wago Addons uploads.
- **v0.10.0:** loot embeds enriched from Wowhead (icon thumbnail, tooltip
  lines), `wowhead` config flag.
- **v0.11.x:** update check with an orange tray badge and one-click
  self-update (`updates.py`). v0.11.0's build failed its leak test (the
  checker's first fetch landed inside it; tests now disable the checker),
  v0.11.1 shipped, v0.11.2 exposed the detached-launch bug, v0.11.3 fixed
  it, and v0.11.3 → v0.11.4 was the first successful live self-update
  (2026-10-03 00:45). Wago answered 403 to the v0.11.3 upload (rate limit
  after a burst of releases, presumably); v0.11.4 uploaded fine.
- **v0.13.0:** companion settings window (WebView2). **v0.14.0:** realtime
  via screenshots replaces the pixel strip (see "Screenshot transport
  contract"); `doink decode`.
- **Later:** community channels via `C_Club` once one exists to test with
  (parked: Battle.net is limited in the beta); rare kills, achievements;
  new PvP ranks and legacy points (user request, 2026-11-02; the events and
  APIs behind both are unknown until launch). (CurseForge packaging via the BigWigs packager: wired up, see Releases.)

## Working style

- Implement one milestone per request. Don't run ahead.
- When the WoW API is uncertain, say so and propose how to verify in-game via
  `/etrace` or `/dump` rather than guessing a signature.
- Keep the addon side small and readable; the developer is learning the WoW API
  through this code.
