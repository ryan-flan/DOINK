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
2. **Companion (Python, runs on the Windows gaming PC, optional)** — reads
   the queue and POSTs to Discord.

The addon is transport-agnostic. The companion decides how it learns about
events. v1 uses a file watcher; later versions may add a combat-log tailer or a
pixel-encoding screen reader for realtime. **Never** write addon code that
assumes a particular transport.

## Repo layout

```
DOINK/
├── CLAUDE.md
├── README.md                 # user-facing install + commands
├── .gitattributes            # * text=auto eol=lf
├── .github/workflows/        # ci.yml: both test suites; release.yml: doink.exe on v* tags
├── addon/tests/test_addon.lua  # offline harness: cd addon && luajit tests/test_addon.lua
├── addon/DOINK/              # junctioned into <WoW>/<flavor>/Interface/AddOns/DOINK
│   ├── DOINK.toc
│   ├── Core.lua              # init, SavedVariables, queue, slash commands
│   ├── Json.lua              # minimal JSON encoder (strings, numbers, bools, tables)
│   ├── Defaults.lua          # default config table + ns.Choices for string options
│   ├── Announce.lua          # in-game chat announcements (guild/officer/party/raid)
│   ├── Options.lua           # settings page via the client's Settings API (no library)
│   ├── Transports/Pixel.lua  # realtime strip encoder (experimental, off by default)
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
    ├── tests/                # python -m unittest discover -s tests
    └── doink/
        ├── runner.py         # the watch-and-post loop (process, run)
        ├── app.py            # controller for the UI: settings, watcher restart
        ├── ui.py             # settings window (tkinter, dark theme)
        ├── tray.py           # Windows tray icon + single-instance lock (ctypes Win32)
        ├── autostart.py      # "Start with Windows": HKCU\...\Run\DOINK
        ├── status.py         # thread-safe state for the tooltip/notifications
        ├── config.py         # loads config.toml (optional, every key optional)
        ├── discover.py       # finds <WoW>/_*_/WTF/Account/*/SavedVariables/DOINK.lua
        ├── pixel.py          # realtime: GDI capture of two bands, strip decoder, meter
        ├── watcher.py        # polls SavedVariables file mtime
        ├── parser.py         # extracts event JSON strings from the Lua file
        ├── discord.py        # embed builders + webhook POST
        └── state.py          # last-seen seq per character (state.json, gitignored)
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
  debug = false,
  webhooks = {                  -- optional; flat so the companion can parse it
    ["*"] = "https://discord.com/api/webhooks/...",              -- all chars
    ["Flano-Whatever"] = "https://discord.com/api/webhooks/...", -- override
  },
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
  - `loot`:      `{"item_id": 123, "link": "|cnIQ3:|Hitem:123:...|h[Name]|h|r", "name": "...", "quality": 3, "qty": 1, "vendor_value": 1234}`
  - `death`:     `{"zone": "Westfall", "subzone": "...", "killer": "Defias Pillager"|null, "environment": "FALLING"|null}`
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

## Announcements (in-game chat)

`Announce.lua` is registered in `ns.Transports` like the pixel strip: Core
calls `Send(json, envelope)` for every emitted event and never knows what the
transport does with it. Rules live in `ns.Defaults.announce` (per-character
overrides via `ns:GetOption("announce", key)`; allowed string values in
`ns.Choices.announce`): `channel` guild|officer|party|raid|off, `level_up`
milestones|all|off, `loot` minimum quality any|uncommon|rare|epic|legendary|off,
`skill_up` max|milestones|all|off, `death`/`quest`/`boss_kill` booleans.
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

## Pixel transport contract (realtime, experimental)

`addon/DOINK/Transports/Pixel.lua` encodes; `companion/doink/pixel.py`
decodes. `addon/tests/fixtures/pixel_levelup.txt` is the Lua encoder's output
for a fixed event; the Lua test asserts it, the Python tests decode it, so the
two implementations can't drift apart. Regenerate with
`DOINK_WRITE_FIXTURE=1 luajit tests/test_addon.lua` when the layout changes.

- **Strip**: `ROWS=3` rows × `N` blocks of `B`×`B` px (`B` default 3; `N =
  clamp(floor((screenWidth - 24)/B), 104, 400)`). Anchored to a user-chosen
  corner (`DOINKDB.realtime.position`, default `topleft`) **12 px in from
  the side edge** and flush with the top/bottom edge: Windows 11 rounds
  window corners by ~8 px and the GDI capture sees the rounded, blended
  pixels (beta 2026-10-02: the last block of every full-width chunk read as
  the desktop behind the corner). Parented to **`UIParent`**
  (children of `WorldFrame` draw beneath every UIParent frame whatever their
  strata; a Details backdrop dimmed and corrupted the strip in beta), strata
  `TOOLTIP`, level 10000, mouse disabled, pixel-perfect via
  `SetIgnoreParentScale(true)` + `SetScale(768/physicalHeight)` (verified:
  exactly 3.0 px blocks at 3438×1408 windowed). Alt-Z hides it with the UI.
- **Bits**: 1 bit per block, 1 = white, 0 = black, opaque.
- **Row 0**: 16-block sync `1010101010110011`, then MSB-first: `version(4)=1,
  flags(4)=0, blocks(16), msg_id(16), chunk_index(8), chunk_count(8),
  payload_len(8), payload_crc16(16), header_crc8(8)`: 104 blocks, rest 0.
  CRC-16/CCITT-FALSE (0x1021, init 0xFFFF) over the chunk payload; CRC-8
  (0x07, init 0) over the 10 header bytes.
- **Rows 1+**: payload bytes MSB-first, row-major, rest 0. Capacity
  `floor((ROWS-1)*N/8)` bytes per chunk (100 at N=400).
- **Message** = the event's JSON string exactly as queued (UTF-8), or a hello
  `{"type":"hello","addon":..,"char":..,"surname":..,"realm":..,"position":..}`
  (plus `"test":true` from `/doink realtime test`). Hellos are shown as
  status, never posted.
- **Timing**: each chunk shows for 250 ms; the outbox (max 8 messages) cycles
  until each message has been shown 4 times, then the strip hides. No idle
  heartbeat. A hello is sent at `PLAYER_LOGIN` and on `/doink realtime on`.
- **Reader**: BitBlt of the top and bottom `BAND_ROWS=24` rows of the client
  area into one DIB section; finds the sync at any x with per-block-size
  regexes (±2 px), fits the grid by least squares and keeps refining it from
  every transition along each row (the sync alone can't pin a fractional
  block width over 400 blocks; snap tolerance 0.45 block, because a
  near-integer width such as 3.02 px jumps a whole pixel every sixty-odd
  blocks), reads each block as the **majority of its central half** (soft
  edges), calibrates black/white on the sync blocks for the header row,
  thresholds each payload row locally (48 px segments), checks both CRCs,
  and if the fitted grid fails, **sweeps** block widths ±0.08 px in 0.01
  steps and the sync position ±1 px until a grid passes both CRCs (dying
  rescales the strip slightly; see beta facts). 8 Hz while WoW is the foreground window, 1 Hz otherwise;
  foreground-only (no `PrintWindow`) by design.
- **Dedupe**: `state.json` gained `missing`: seqs below `last_seen` not yet
  posted. Realtime posts call `mark_posted(key, seq, first_sight_backlog)`;
  the file pass posts `seq > last_seen or seq in missing` and prunes gaps the
  file can't fill. Both workers share one `State` and one `WebhookPool`.
- **Opt-in on both sides**: `DOINKDB.realtime.enabled` (addon) and
  `realtime` in `config.toml` (companion), both default false. Nothing is
  created in game until `/doink realtime on`.

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

- Python 3.12+, run from **WSL** (`python3`; there is no Windows Python on the
  dev machine). Run: `cd /mnt/c/dev/DOINK/companion && python3 main.py`.
  Tests: `python3 -m unittest discover -s tests`. The PyInstaller .exe build
  (M4) will need Windows Python.
- Config paths may be written Windows-style (`C:\...`); `config.native_path`
  maps them to `/mnt/c/...` under WSL, so one config works in both places.
- Poll the SavedVariables file's mtime every ~2s. No OS file-watch hooks
  (inotify across WSL `/mnt/c` is unreliable; stat polling is verified to see
  Windows-side writes immediately).
- On change: parse → events with `seq > state[char].last_seen` → POST in order →
  persist state. Never re-post on restart.
- Parsing: find the `events = { ... }` blocks, extract Lua string literals
  (handle `\"` and `\\` escapes), `json.loads` each. Skip unparseable entries
  with a warning rather than crashing.
- Discord: one embed per event, colour by class (use the standard WoW class
  colours; loot uses item-quality colour instead), Wowhead links for items and
  quests (`https://www.wowhead.com/forever/item=<id>`, `/quest=<id>`;
  verified), footer `Char-Realm`. Respect the
  webhook rate limit (~30/min); batch if many events arrive from one reload.
- `--dry-run` prints embeds to stdout instead of POSTing.
- Zero config is the goal: `config.toml` is optional and so is every key in
  it (`savedvariables_path`, `webhook_url`, `dry_run`, `poll_interval`,
  `max_backlog`). Without a path, `discover.py` scans standard WoW folders on
  every drive, keeping only flavors where `Interface/AddOns/DOINK` exists, and
  watches every account's file. Notifier toggles are **not** companion config.
- `config.toml` and `state.json` live next to `main.py`, or next to the .exe
  when frozen (`main.app_dir()`).
- Webhooks are per URL (`WebhookPool`) so each keeps its own rate limiting.
  A character with no resolvable webhook logs an error and its events stay
  queued (state not advanced) until one is set.
- Standard library only (`urllib`, `tomllib`); no dependencies.
- The watcher only reports a change once `(mtime, size)` has held still for one
  poll, so it never reads a half-written file.
- Up to 10 embeds per webhook message (Discord's limit), at least 2s between
  messages, and 429 `retry_after` is respected. State is saved after each
  message.
- First time a character is seen, post at most `max_backlog` (default 10) of
  its queued events. If the newest seq in the file is below `file_seen`
  (the highest seq a previous file pass showed; SavedVariables wiped), treat
  the character as new. Never compare with `last_seen` for this: realtime
  posts run ahead of the file until the next `/reload`, and a companion
  restart in that window once re-posted ten events (2026-10-02).
- **Two modes.** `python3 main.py` (WSL/dev) is a console app. The packaged
  Windows build (`doink.exe`, `--noconsole`) and `--tray` run as a tray app
  with three threads: Tk owns the main thread (settings window), the tray
  runs its own Win32 message loop, and the watcher runs with a stop `Event`.
  Tray → Tk goes through a `queue.Queue` polled with `root.after` (Tk isn't
  thread-safe); watcher → both goes through `Status`. `App` owns config and
  the watcher: webhook changes apply live (`config.webhook_url` is read per
  post), folder changes restart the watcher.
- Settings window writes `config.toml` via `config.save_settings` (comments
  aren't preserved). Webhook precedence: in-game per-char > in-game `*` >
  settings window. It opens itself on first run (`App.needs_setup()`), and
  "WoW not found" is a state the window resolves, not a fatal error. Tray
  mode logs to `doink.log` next to the exe (rotating, ~2 MB max), never
  `print`s (there's no stdout), and shows startup errors in a message box.
- Tray code is ctypes against Win32 with explicit `argtypes`/`restype` on
  every call (64-bit handles). Keep the companion dependency-free; no
  pystray/Pillow. Win32 behaviour is only testable on the Windows CI job
  (`tests/test_windows.py`).
- Realtime reading has a budget: capture only the two bands, allocate the
  buffer once, no per-pixel Python objects, decode ≈ 1 ms per capture at
  3421 px (≈ 5 ms on pure noise). Measured live (2026-10-02, 3438×1408
  window): 7.6 captures/s, 16.7 ms wall per capture (BitBlt waiting on the
  GPU) but ~1 ms CPU, 1.25% CPU, 46 MB, 54 GDI handles, flat. `ResourceMeter`
  samples CPU time per capture (not wall time: the GPU wait isn't cost),
  working set and GDI handle count; `RealtimeWorker` stops itself (and says
  why in the settings window) if captures average > 25 ms CPU, GDI handles
  grow > 50, or the working set is > 100 MB over baseline **and still
  climbing** (5 rising samples): the whole process is measured and the
  settings window adds ~80 MB of Tk in one step, which tripped the first
  version of the valve. Baseline is the 3rd sample, after start-up. Diagnostics: `-v`
  logs every rejected strip candidate with the reason; `DOINK_DUMP_BANDS=<dir>`
  writes the raw bands it failed on, replayable with `pixel.decode`. The Windows CI job runs a
  1000-capture leak test. Status updates from the reader never notify the
  tray except on an on/off change.
- Trust is a feature: autostart is off by default and writes exactly one
  per-user Run value; a named mutex stops a second copy (double posts); the
  exe has a version resource and icon; releases ship a `.sha256`. Keep the
  README's "What doink.exe does" section accurate when behaviour changes.
- PyInstaller `--onedir` (not `--onefile`: fewer antivirus false positives,
  faster start). The release workflow smoke-tests the built exe.

## Dev setup (reference)

- Repo lives at `C:\dev\DOINK` (WSL: `/mnt/c/dev/DOINK`).
- Addon folder is junctioned:
  `mklink /J "<WoW>\<flavor>\Interface\AddOns\DOINK" "C:\dev\DOINK\addon\DOINK"`
- In-game helpers: BugSack + BugGrabber (errors), DevTool (`/dev`), built-in
  `/etrace`, `/dump`, `/fstack`, `/console scriptErrors 1`.
- SavedVariables path:
  `<WoW>\<flavor>\WTF\Account\<ACCOUNT>\SavedVariables\DOINK.lua`
- Run all tooling (`git`, `gh`, `python3`) in WSL; none of it is on the
  Windows PATH. From PowerShell: `wsl -e sh -c "cd /mnt/c/dev/DOINK && ..."`.
- Remote: https://github.com/ryan-flan/DOINK (public), default branch `main`.
- CurseForge project ID: 1721438 (repo variable `CURSEFORGE_PROJECT_ID`).
  Wago Addons project id: repo variable `WAGO_PROJECT_ID` (8-character id
  from the Wago developer dashboard).

## Releases

1. Bump the version in all three places: `addon/DOINK/DOINK.toc`
   (`## Version`), `companion/doink/__init__.py`, `companion/pyproject.toml`.
   Commit as `build: release vX.Y.Z`.
2. `git tag -a vX.Y.Z -m "DOINK vX.Y.Z"` and `git push origin vX.Y.Z`.
3. `release.yml` runs two jobs:
   - `build` (Windows): tests, builds `doink.exe`, publishes
     `DOINK-vX.Y.Z.zip` (AddOns/, Companion/, README, LICENSE) + `.sha256`
     as a GitHub release. Notes are generated from commits.
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
     set` through WSL once stored an empty value.

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

- [x] Flavor folder name: `_classic_beta_` (`.flavor.info`: `wow_classic_beta`).
  Install: `C:\Program Files (x86)\World of Warcraft\`. Expect a different
  folder at launch.
- [x] TOC `## Interface:` version: `16001` (client `1.60.1 70124`, Sep 29 2026).
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
- [ ] `ENCOUNTER_END` fires in Forever dungeons? ______
- [x] `QUEST_TURNED_IN` fires with questID/xp on a real turn-in, and the
  title resolves (quest 818 "A Solvent Spirit", 625 xp).
  `C_QuestLog.GetQuestInfo` returns `nil` for quests not in the log.
- [x] During `PLAYER_LEVEL_UP`, `UnitLevel("player")` still returns the
  **old** level. Core tracks `ns.knownLevel` from the event for the envelope.
- [x] `SKILL_RANK_UP` = `Your skill in %s has increased to %d.` (keeps its
  period, unlike the loot strings).
- [x] `PLAYER_DEAD` does **not** re-fire on `/reload`: three real deaths with
  a reload in between produced exactly three events (2026-10-02). No guard
  needed. Deaths post via realtime within ~1 s.
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
  sides). See "Pixel transport contract". Not done: `PrintWindow` capture
  for an alt-tabbed WoW; `Screenshot()`-based capture as a fallback.
- **v0.7.0:** in-game chat announcements (`Announce.lua`); the addon is now
  useful without the companion, and the README/CurseForge page lead with
  that. See "Announcements".
- **v0.8.0:** settings page (`Options.lua`, native Settings API).
- **v0.9.0:** `death.killer` from the death recap, environmental death
  lines, `file_seen` wipe check. **v0.9.1:** reader copes with a slightly
  rescaled strip (realtime deaths).
- **Later:** community channels via `C_Club` once one exists to test with
  (parked: Battle.net is limited in the beta); rare kills, achievements.
  (CurseForge packaging via the BigWigs packager: wired up, see Releases.)

## Working style

- Implement one milestone per request. Don't run ahead.
- When the WoW API is uncertain, say so and propose how to verify in-game via
  `/etrace` or `/dump` rather than guessing a signature.
- Keep the addon side small and readable; the developer is learning the WoW API
  through this code.
