# DOINK: tell your guild about your adventures

Announces your milestones in guild chat as they happen, with real item links:

```
[Guild] Paul Hebbs: Ding! Level 20.
[Guild] Paul Hebbs: Looted [Thunderfury, Blessed Blade of the Windseeker]!
[Guild] Paul Hebbs: Ragnaros down! (Molten Core, 40 players)
[Guild] Paul Hebbs: Died in Westfall.
```

Level milestones (10, 20 … 60), epic+ loot, boss kills and deaths by default;
quest turn-ins and skill-ups if you want them. Guild, officer, party or raid
chat. Rate-limited, so a loot burst can never flood anyone. Nothing leaves the
game.

## Commands

- `/doink announce test`: whispers you a sample of every announcement
- `/doink announce guild|officer|party|raid|off`
- `/doink announce loot rare`, `/doink announce quest on`, `/doink announce level_up all` …
- `/doink`: status

## Optional: mirror everything to Discord

With the free **DOINK companion** (Windows, open source, MIT) the same events
also post to a Discord channel as rich embeds with Wowhead links. WoW addons
can't talk to the internet, so the companion does it from outside the game.
Setup takes a minute: run `doink.exe`, paste your webhook into its settings
window, click *Send test message*.

**https://github.com/ryan-flan/DOINK/releases/latest**

Source, issues and the companion: https://github.com/ryan-flan/DOINK
