# DOINK: your milestones in guild chat, and in Discord

DOINK is an addon for **World of Warcraft: Forever** that tells your guild
about your adventures. It watches for nine kinds of in-game event
(level-ups, loot, deaths, quest turn-ins, boss kills, profession skill-ups,
new mounts, duel and Mak'gora wins, and battleground wins) and announces the
ones you choose in guild, officer, party or raid chat the moment they
happen, with real item links:

```
[Guild] [Flano]: Ding! Level 20.
[Guild] [Flano]: Looted [Thunderfury, Blessed Blade of the Windseeker]!
[Guild] [Flano]: Ragnaros down! (Molten Core, 40 players) First kill!
[Guild] [Flano]: Got a new mount: [Gray Wolf]!
[Guild] [Flano]: Won a Mak'gora against Grakkor Highmountain.
[Guild] [Flano]: Killed by Defias Pillager in Westfall.
```

**Discord too.** The free, open-source
[DOINK companion](https://github.com/ryan-flan/DOINK/releases) mirrors the
same events to a Discord channel of your choice as class-coloured embeds
with Wowhead links, so your guild's Discord sees every ding, epic and boss
kill even when nobody is online. See
[Discord mirroring](#discord-mirroring-with-the-doink-companion).

> **Two parts, one optional.** The addon alone does all the guild chat
> announcements. **Discord posting requires the companion**, a separate
> program (Windows, or Linux) you download from the GitHub releases page and run
> alongside the game: WoW addons cannot reach the internet, so without the
> companion nothing goes to Discord. Install the addon from here; get the
> companion from GitHub if you want Discord.

No dependencies and no libraries: install it and it works. Settings are on
a normal options page (Esc → Options → AddOns → DOINK) and can also be set
with `/doink` commands.

**Guild already linked to Discord?** Blizzard's guild chat link shows DOINK's
lines in your Discord channel with no companion at all; see
[below](#already-linking-your-guild-to-discord).

## Credit: Dink

DOINK is heavily inspired by [Dink](https://github.com/pajlads/DinkPlugin),
the RuneLite plugin by [pajlads](https://github.com/pajlads) that posts your
Old School RuneScape achievements to Discord. The idea of an addon that
notices your milestones and tells your friends, the Discord embeds, the
per-event toggles and the name all come from Dink. DOINK is not affiliated
with the Dink project; it's a from-scratch WoW take on the same idea, built
within what a WoW addon is allowed to do. If you play OSRS, go use Dink.

## Install

Install **DOINK** from CurseForge or Wago Addons (or through WowUp,
which lists it from GitHub Releases), or download the newest
`DOINK-<version>-forever.zip` from
[Releases](https://github.com/ryan-flan/DOINK/releases) and copy its
`DOINK` folder into your WoW AddOns folder, e.g.
`C:\Program Files (x86)\World of Warcraft\_forever_\Interface\AddOns\`
(during the beta the folder was `_classic_beta_`).
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
| Loot | Your own `You receive loot:` message | `Looted [item link]!` (`x3` appended for stacks) | Epic quality or better (see below) |
| Death | `PLAYER_DEAD`, killer from the client's death recap | `Killed by Defias Pillager in Moonbrook, Westfall.`; falling, drowning, fatigue, fire, lava and slime get their own lines (`Forgot I couldn't fly. Died in Thousand Needles.`); `Died in Moonbrook, Westfall.` when the recap is empty | Yes |
| Boss kill | `ENCOUNTER_END` with success | `Ragnaros down! (Molten Core, 40 players)`, plus `First kill!` the first time this character kills that boss | Yes (kills only, never wipes) |
| Quest turn-in | `QUEST_TURNED_IN` | `Completed quest: A Threat Within.` | No |
| Skill-up | `Your skill in X has increased to N.` | `Blacksmithing maxed at 300!` / `Blacksmithing 150/225.` | No |
| Mount | A new mount learned (`NEW_MOUNT_ADDED`), or a mount item arriving in your bags | `Got a new mount: [Gray Wolf]!` / `Bought a new mount: [Gray Wolf]!` (bought = a vendor window was open) | Yes (at most one every two minutes) |
| Duel | The game's duel result message | `Won a duel against Grakkor Highmountain.` (`(they ran)` if they fled) / `Won a Mak'gora against Grakkor Highmountain.` | Yes (wins only) |
| Battleground | The end of a battleground match | `Won Warsong Gulch!` | Yes (wins only) |

The defaults are deliberately conservative because guild chat is shared.
Each rule can be changed per character:

- Level-up: `milestones` (default), `all`, `off`
- Loot: minimum quality `any`, `uncommon`, `rare`, `epic` (default), `legendary`, or `off`
- Skill-up: `max` (only when a skill reaches its cap), `milestones` (75/150/225/300), `all`, `off` (default)
- Death, boss kill, quest, mount, duel, battleground: `on` or `off`

The announcement rules work on what the addon records (see
[Notifiers](#notifiers-what-gets-recorded-for-discord)), so a few event
settings apply to chat too. Loot: a rare is only recorded when it is Bind
on Equip or sells for 1g or more (a soulbound blue only matters to you),
legendaries always
when they are a weapon or armour and never when they are a material; with
the default `epic` rule, chat shows epics and legendaries. Duel and
battleground losses are not recorded unless you turn off `wins_only`
(`/doink set duel wins_only off`); then chat says `Lost a duel to ...` /
`Lost Warsong Gulch.`. Boss kills can be limited to first kills with
`/doink set boss_kill first_kill_only on`.

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
| `/doink enable <type>` / `disable <type>` | Turn an event type off entirely (`level_up`, `loot`, `death`, `quest`, `boss_kill`, `skill_up`, `mount`, `duel`, `battleground`) |
| `/doink options [type]` | Show the event settings (for example the loot quality and vendor-value thresholds used for Discord) |
| `/doink set <type> <option> <value>` | Change one, e.g. `/doink set loot min_quality 4` |
| `/doink reset <type>` | Back to defaults |
| `/doink test <type>` | Generate a fake event of that type (whispered to you; also queued for the companion, marked as a test) |
| `/doink dump [n]` | Print the last n queued events |
| `/doink debug` | Toggle printing of every event the addon sees, for troubleshooting |
| `/doink flush` | Reload the UI so saved variables are written |
| `/doink webhook [here] <url>` / `clear` | Only for the Discord companion: store a webhook URL account-wide, or for this character |
| `/doink realtime on` / `off` / `test` | Only for the Discord companion: post each event within seconds instead of at the next reload (see [Instant posts](#instant-posts-realtime)) |

Settings are per character and take effect immediately, whether changed on
the settings page or by command; the two are the same settings.

## Compatibility

- Built for World of Warcraft: Forever (client 1.60.1, Interface 16001).
  It uses the Classic-era API only.
- Chat patterns are built from the game's own strings (`LOOT_ITEM_SELF`,
  `SKILL_RANK_UP`, `DUEL_WINNER_KNOCKOUT`), so loot, skill and duel
  detection work on any client language.
- Forever surnames are supported: characters are tracked by full name, and
  whispers use the full name.

## Known limitations

- The killer in a death announcement comes from the client's death recap
  (the same data as the "Death Recap" button). Addons cannot read the
  combat log in Forever, so if the recap is empty the line omits the killer.
- Announcements can't be sent to Blizzard Communities yet.
- Loot and skill-ups that happen during a boss encounter aren't detected:
  Forever hides chat text from addons while an encounter is in progress.
  Boss kills themselves and anything after the fight work normally.
- Boss kills are detected through `ENCOUNTER_END`, verified in Forever
  dungeons during the beta. Raids haven't been tested yet.
- Mounts, duels (including Mak'gora) and battlegrounds are new in v0.12
  and couldn't be fully tested in the beta; they are built to try more than
  one route where the game's behaviour wasn't known. Reports welcome.

## Already linking your guild to Discord?

Forever can link guild chat to a Discord channel itself. A player turns on
*Esc → Options → Social → Enable Discord Functionality* (this links their
Battle.net and Discord accounts); a guild leader or officer who is also an
admin of the Discord server sets it up under *Guild Settings → Guild
Control → Discord Settings* (server, channel, Link).

Once that's done, every DOINK line you send to guild chat also shows in the
linked channel, as a plain message from you, as long as you have linked
your own account. No companion needed.

The companion is still worth it if you want rich embeds with item icons and
Wowhead links, a different channel per character, or events you'd rather
not put in guild chat (quests, skill-ups, every rare drop).

## Discord mirroring with the DOINK companion

**Required for Discord.** Nothing in this section works with the addon
alone. Discord posting needs the DOINK companion running on your PC
(Windows, or Linux if you play through Lutris, Steam/Proton or Wine),
because WoW addons cannot access the network. The addon's guild chat
announcements need nothing extra.

Everything the addon announces can also land in Discord:

> **Flano Wren looted Wolfmaster Cape**
> Item Level 27 · Binds when picked up · Back · 22 Armor · +3 Stamina ·
> Requires Level 22 · Equip: +10 Attack Power.
> Vendor value: 12s 15c
> Flano Wren-Whatever

(an embed in the item's quality colour, with the item's icon, its tooltip
from Wowhead and the name linking to Wowhead; level-ups and deaths use your
class colour)

WoW addons cannot access the network, so Discord posting is done by a
separate, optional program, the **DOINK companion**. Download
`DOINK-<version>.zip` from the release marked **Latest** on the
[GitHub releases page](https://github.com/ryan-flan/DOINK/releases): it
contains the companion (`Companion\doink.exe`) and a copy of the addon.
(Releases that only update the addon don't carry it.)
Linux players download `DOINK-<version>-linux-amd64.tar.gz` instead (see
[On Linux](#on-linux)). The companion is not on the addon sites and is not
required for anything above.

What it is: one small program (`doink.exe` on Windows, `doink` on Linux;
written in Go, source in this repository, MIT) that sits in the system tray
on Windows.
Every two seconds it checks whether the game has written `DOINK.lua`; when
it has, it reads the event buffer and posts each new event to a Discord
webhook you provide, as an embed (class-coloured, with a Wowhead link for
items and quests). It remembers what it has posted so nothing is posted
twice. It reads only `DOINK.lua` (and, with
[instant posts](#instant-posts-realtime), DOINK's own screenshots) and
writes only its own three files (settings, posting history, log) in its
own folder; see
[What doink.exe does](#what-doinkexe-does-and-doesnt) for its network
requests.

> **Discord posts arrive when WoW saves addon data:** on `/reload`, logout,
> or exit. That's a WoW limitation, not a setting. Play normally and your
> session posts when you log out, or type `/doink flush` to post right away.
> Guild chat announcements are always instant.
>
> Want posts within seconds instead? Turn on
> [instant posts](#instant-posts-realtime).

### Setting it up

1. Download `DOINK-<version>.zip` from the release marked **Latest** on
   [Releases](https://github.com/ryan-flan/DOINK/releases), move its
   `Companion` folder somewhere permanent that you can write to, e.g.
   `Documents\DOINK`, and run `doink.exe`. A gold **D** appears in the
   system tray (click `^` if it's hidden). Until you've set a webhook it
   shows a "no webhook yet" notification; that's expected.
2. In Discord: *Server Settings → Integrations → Webhooks → New Webhook*,
   choose a channel, *Copy Webhook URL*. Then, in game:
   `/doink webhook <url>` and `/doink flush`. (Or paste it into the
   companion's settings window: right-click the tray icon → **Settings**,
   then **Send test** to check it.)
3. Optional: tick **Start with Windows** in the settings window (or the
   tray menu).

DOINK finds your WoW install by itself; if it can't, the tray icon says so.
Choose your World of Warcraft folder in the settings window (**Browse…**).

To check the whole loop in game: `/doink test levelup`, then `/doink flush`.
A `[TEST]` post should appear a few seconds after the reload. To see what
every embed looks like without logging in, run `doink.exe test all` from a
command prompt (or `test loot`, `test death`, ...): it posts the same
`[TEST]` events as `/doink test` to your webhook.

Hover over the tray icon for the last post or the current problem; if
something goes wrong (no webhook, Discord unreachable) you get a Windows
notification once. **Click** the icon for the settings window.
**Right-click** for Settings, Open log, Open DOINK folder, Start with
Windows, Check for updates and Quit.

The **settings window** has the webhook (with a **Send test** button), the
WoW folder, Start with Windows, and the last few posts. It follows your
Windows light or dark theme. It saves to `config.toml`, keeping anything
else you wrote there, and changes apply without restarting DOINK. Every
other setting is in `config.toml` (link at the bottom of the window). The
window is drawn by Microsoft Edge WebView2, which comes with Windows 11 and
current Windows 10; without it, Settings opens `config.toml` instead.

**Updates.** DOINK checks GitHub once a day for a newer companion release. When
there is one, the tray icon gets an orange dot and the tray menu (and the
settings window) offers **Update to vX**. Click it and DOINK downloads the
release, checks its SHA-256, replaces `doink.exe` and restarts itself in a
few seconds. It never installs anything unless you click. Add
`update_notify = true` to `config.toml` if you'd also like a Windows
notification. Your settings, `state.json` and log are never touched, and
neither is the addon, which updates through CurseForge or Wago like any
other addon; addon-only releases don't prompt a companion update.
(Versions before 0.15 can't do this: update those once by hand, from the
zip on the release marked **Latest**.)

**One channel per character?** Set a webhook in game with
`/doink webhook here <url>` on that character. In-game webhooks take priority
over the one in `config.toml`: per character first, then
`/doink webhook <url>` (all characters), then `webhook_url` in
`config.toml`. Webhook changes reach the companion on the next reload.

### Instant posts (realtime)

Off by default. With `/doink realtime on` (or **Post to Discord
instantly** on the settings page), each event reaches Discord a few
seconds after it happens, without a reload. Needs companion v0.14 or later.

How it works: an addon can't send anything out of the game, but it can
take a screenshot. For each event, DOINK flashes a thin black-and-white
strip (about 8 px tall) along the top of the screen for a single frame and
takes a screenshot; you'll see the game's usual "Screen captured" message.
The companion finds that screenshot in WoW's `Screenshots` folder, reads
the code, posts the event and deletes the screenshot. Your own screenshots
are never touched: the companion deletes only screenshots that contain a
valid DOINK code. If the companion isn't running, DOINK's screenshots stay
in the folder until it next starts and cleans them up (as long as they're
under 14 days old).

`/doink realtime test` takes one test screenshot; the companion answers
with a "Realtime works" notification and posts nothing. Nothing is posted
twice: when you later `/reload`, events that already arrived this way are
skipped. If the game ever blocks addon screenshots (during some boss
fights, say), DOINK turns realtime off by itself and tells you; events then
post at the next reload as usual. `realtime = false` in `config.toml` stops
the companion reading screenshots.

Optionally the screenshot can go to Discord too: with
`attach_screenshot = true` in `config.toml`, the event's embed shows the
screenshot as its image (it includes the thin strip along the top edge).
It is off by default, since a screenshot shows whatever was on your screen,
and it only works together with instant posts: events that arrive at a
reload have no screenshot. Screenshots over 8 MB are left off, the event
posts without. The file is deleted afterwards as usual.

### On Linux

Download `DOINK-<version>-linux-amd64.tar.gz` from the release marked
**Latest**, unpack it somewhere
permanent and run `./doink` (a console program; keep it running while you
play). It finds World of Warcraft inside Lutris, Steam (Proton) and Wine
prefixes by itself; if yours lives elsewhere, set `wow_dir` in a
`config.toml` next to `doink` (see `config.example.toml`). Set the webhook
in game as above, or with `webhook_url` in `config.toml`.
`./doink --test-webhook <url>` posts a test message, and `./doink test all`
posts every sample embed. There's no tray on Linux; it logs to the
terminal.

To start it with your desktop session, a systemd user unit does the job.
Save this as `~/.config/systemd/user/doink.service` (adjust the path):

```ini
[Unit]
Description=DOINK companion (WoW: Forever events to Discord)

[Service]
ExecStart=%h/DOINK/doink
Restart=on-failure
RestartSec=30

[Install]
WantedBy=default.target
```

Then `systemctl --user enable --now doink`, and
`journalctl --user -u doink -f` to watch its log. If it can't find WoW it
exits with an error in that log; set `wow_dir` and it starts on the next
retry.

### Notifiers (what gets recorded for Discord)

These decide which events are recorded for Discord; the chat announcement
rules above are applied on top.

| Type | Posts when | Options (defaults) |
|---|---|---|
| `level_up` | You level up | `milestones_only` off (on = every 10 levels) |
| `loot` | You loot something good | `min_quality` 3 (rare), `min_vendor_value` 1g (whole stack). Either one is enough. `rares_boe_only` on: a rare counts only if it is Bind on Equip (or the game doesn't say). Legendaries always post when they are a weapon or armour, never when they aren't |
| `death` | You die | |
| `quest` | You turn in a quest | |
| `boss_kill` | Your group kills a dungeon or raid boss (the embed lists your party and says `First kill!` the first time) | `first_kill_only` off |
| `skill_up` | A skill reaches a milestone | `milestones_only` on (75/150/225/300) |
| `mount` | You get a new mount | |
| `duel` | A duel or Mak'gora ends | `wins_only` on |
| `battleground` | A battleground match ends | `wins_only` on |

Money options accept `1g50s`, `75s`, `30c` or plain copper. Quality: 0 poor,
1 common, 2 uncommon, 3 rare, 4 epic, 5 legendary.

### Companion settings

Settings live in `config.toml` next to `doink.exe` (the settings window
edits the webhook and WoW folder there; the link at its bottom opens the
file). Every key is optional: `webhook_url`, `wow_dir`, `dry_run`,
`max_backlog`, `poll_interval`, an exact `savedvariables_path`, `wowhead`,
`realtime`, `attach_screenshot`, `update_check`, `update_notify`; see
`config.example.toml`.
DOINK picks up changes while it runs, except `dry_run`, `wowhead`,
`update_check` and `update_notify`, which need a restart.

The companion remembers what it has posted (`state.json`), so restarting it
never double-posts. The first time it sees a character it posts only the
newest 10 queued events.

### What doink.exe does (and doesn't)

- **One file.** `doink.exe` is a single program with nothing to install
  and no `_internal` folder (versions up to v0.11 were Python and had one;
  if you update by hand you can delete it).
- **Reads** `WTF\Account\*\SavedVariables\DOINK.lua` in your WoW folders,
  and the screenshots in WoW's `Screenshots` folder (those from the last
  14 days when it starts, then new ones) to find DOINK's realtime codes.
- **Deletes** only screenshots that contain a valid DOINK realtime code,
  after posting them. `realtime = false` turns this off.
- **Writes** only inside its own folder: `config.toml` (your settings),
  `state.json` (what it has already posted), `doink.log` (capped at ~2 MB;
  the Linux build logs to the terminal instead). The one exception: the
  settings window's WebView2 keeps its browser data in
  `%LOCALAPPDATA%\DOINK\WebView2`, created the first time you open it.
- **Network:** HTTPS posts to your Discord webhook (with
  `attach_screenshot = true`, a realtime event's screenshot is uploaded
  with it; off by default); one small HTTPS request
  to Wowhead per looted item (its id only) for the loot embed's icon and
  tooltip (`wowhead = false` in `config.toml` turns that off); and once a
  day one HTTPS request to GitHub for its list of recent releases, to learn
  the newest companion version (`update_check = false` turns that off). Only when you click **Update**
  it also downloads that release's zip and its `.sha256` from GitHub,
  verifies them, and replaces `doink.exe` with the `doink.exe` inside
  (the old one is kept as `doink.exe.old` until the next start, then
  deleted). It never does that by itself. No telemetry. It doesn't listen on any port.
  The settings window is a local page built into `doink.exe`; it loads
  nothing from the internet.
- **Start with Windows** adds one per-user registry value,
  `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\DOINK`. No admin
  rights. Untick it in the tray menu, or in Task Manager → Startup apps.
- **Screen:** it never looks at or captures your screen; realtime works
  through the screenshots the addon itself takes.
- **Uninstall:** untick Start with Windows, quit, delete the folder (and
  `%LOCALAPPDATA%\DOINK` if you opened the settings window).

Every release is built from this repository's source by
[GitHub Actions](.github/workflows/release.yml); the build log is public on
the release's workflow run. Check your download against the `.sha256` file on
the release page:

```powershell
Get-FileHash DOINK-v0.12.0.zip -Algorithm SHA256
```

> Windows SmartScreen may warn about `doink.exe` because it isn't code-signed
> (*More info → Run anyway*). Code signing costs money per year; it may come
> later.

## How it works: what is in the package

One folder, `DOINK`, 16 files, about 2,200 lines of Lua, no third-party code:

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
| `Notifiers/Mount.lua` | Listens to `NEW_MOUNT_ADDED`, `CHAT_MSG_LOOT`, `MERCHANT_SHOW` and `MERCHANT_CLOSED`. |
| `Notifiers/Duel.lua` | Listens to `CHAT_MSG_SYSTEM`. |
| `Notifiers/Battleground.lua` | Listens to `UPDATE_BATTLEFIELD_STATUS`, `PVP_MATCH_COMPLETE` and `PLAYER_ENTERING_WORLD`. |
| `Transports/Screenshot.lua` | Optional, off by default: [instant posts](#instant-posts-realtime) for the companion. Listens to `SCREENSHOT_SUCCEEDED`, `SCREENSHOT_FAILED`, `ADDON_ACTION_BLOCKED` and `ADDON_ACTION_FORBIDDEN`; creates nothing unless enabled. |

The addon registers only the events listed above (plus `ADDON_LOADED`,
`PLAYER_LOGIN` and `PLAYER_LEVEL_UP`). It does not register the combat log event, which Forever
protects, and it does not hook or replace any Blizzard function. It makes
no network connections (addons can't) and reads nothing from disk. With
instant posts on, it takes screenshots, which the game saves in its
`Screenshots` folder.

## How it works: what the addon stores

Everything is kept in one saved variable, `DOINKDB`, written by the game to
`WTF\Account\<account>\SavedVariables\DOINK.lua` on `/reload` and logout:

- Per character: your settings (only the ones you changed), a counter,
  which bosses you have killed (for `First kill!`), and a ring buffer of the
  last 500 events as JSON text (type, time, character, realm, class, level,
  and event details such as item id and name). This buffer exists for the
  optional companion; the addon itself never reads it back.
- Account-wide: the debug flag, whether realtime is on (off by default;
  while a screenshot is being taken, also your screenshot format if DOINK
  had to switch it, so it can be put back), and any Discord webhook URL you set with `/doink webhook`. If you never set
  one, nothing is stored there.

The addon does not store chat or anything about other characters, with two
exceptions inside events: a boss kill lists your party members' names,
classes and levels, and a duel records your opponent's name.

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
companion/         the companion (Go; BurntSushi/toml, x/sys, go-webview2)
```

```bash
devbox run test                      # both suites (devbox provides Go and LuaJIT)
cd addon && luajit tests/test_addon.lua
cd companion && go test ./...
cd companion && go run ./cmd/doink --dry-run --once --config <path>/config.toml
```

The tray, autostart and single-instance code (`companion/internal/tray`,
`internal/autostart`) talks to Win32 directly through `golang.org/x/sys`;
its tests run on the Windows CI job. The icon is drawn by
`companion/assets/make_icon.py` (a one-off; needs Python with Pillow).

Push a `v*` tag to publish a GitHub release: the companion is built when
`companion/VERSION` equals the tag, and the addon is packaged and uploaded
to CurseForge and Wago Addons when the TOC version does, so either half
can ship on its own. Design notes and the event data contract are in
[CLAUDE.md](CLAUDE.md).

## License

[MIT](LICENSE)
