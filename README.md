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
> when you log out, or type `/doink flush` to post right away.

## Install

1. Download `DOINK-<version>.zip` from
   [Releases](https://github.com/ryan-flan/DOINK/releases) and unzip it.
2. Copy `AddOns\DOINK` into your WoW AddOns folder, e.g.
   `C:\Program Files (x86)\World of Warcraft\_classic_beta_\Interface\AddOns\`.
   Restart WoW completely (new addons are only picked up on start).
3. In Discord: *Server Settings → Integrations → Webhooks → New Webhook*,
   choose a channel, *Copy Webhook URL*.
4. In game, paste it:
   ```
   /doink webhook https://discord.com/api/webhooks/...
   /doink flush
   ```
5. Run `Companion\doink.exe` and leave it open while you play. It finds your
   WoW install by itself.

To check everything works: `/doink test levelup`, then `/doink flush`. A
`[TEST]` post should appear in your channel a few seconds after the reload.

> Windows may warn about `doink.exe` because it isn't code-signed
> (*More info → Run anyway*). It's built from this repo by
> [GitHub Actions](.github/workflows/release.yml).

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

None are needed. To override, copy `config.example.toml` to `config.toml`
next to `doink.exe`. You can set the SavedVariables path (if WoW isn't in a
standard folder), a fallback webhook, and `dry_run` (print instead of post).
`doink.exe --dry-run` and `--once` work too.

The companion remembers what it has posted (`state.json`), so restarting it
never double-posts. The first time it sees a character it posts only the
newest 10 queued events.

## Known limitations

- Not realtime (see above). The beta client's chat and combat log files stay
  empty during play, so there's no faster route yet.
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
cd companion && python3 main.py --dry-run
```

Push a `v*` tag to build `doink.exe` and publish a release. Design notes and
the event data contract are in [CLAUDE.md](CLAUDE.md).
