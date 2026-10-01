# DOINK: Discord notifications for WoW: Forever

Post your adventures to a Discord channel: level-ups, rare loot, deaths,
quest turn-ins, boss kills and profession milestones. Inspired by Dink for
RuneLite.

## ⚠ Needs the free DOINK companion app (Windows)

WoW addons can't talk to the internet, so this addon only *records* events.
The small **DOINK companion** reads them and posts to Discord. Get it from
GitHub (open source, MIT, no installer, no admin rights):

**https://github.com/ryan-flan/DOINK/releases/latest**

Setup takes a minute: run `doink.exe`, paste your Discord webhook into its
settings window, click *Send test message*. Full guide and what the companion
does (and doesn't) on the GitHub page.

## Posts arrive when WoW saves

WoW only writes addon data on `/reload`, logout or exit, so posts arrive
then: play normally and your session posts when you log out, or type
`/doink flush` to post right away.

## Commands

- `/doink` status · `/doink options` settings · `/doink set loot min_quality 4`
- `/doink enable|disable <type>` for level_up, loot, death, quest, boss_kill, skill_up
- `/doink webhook here <url>` a different Discord channel for this character
- `/doink test <type>` then `/doink flush` to check your setup

Source, issues and the companion: https://github.com/ryan-flan/DOINK
