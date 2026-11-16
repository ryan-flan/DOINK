# DOINK events

This is the reference for the JSON that DOINK produces for every recorded
event. You need it if you set `forward_url` in the companion's
`config.toml` to receive events in your own bot, script or spreadsheet.

A machine-readable version is in
[`event.schema.json`](event.schema.json) (JSON Schema, draft 2020-12).

## How events reach you

With `forward_url` set, the companion sends one HTTP request per event:

| | |
|---|---|
| Method | `POST` to your `forward_url` |
| `Content-Type` | `application/json` |
| `User-Agent` | `DOINK-companion/<version>` |
| Body | One event: a single JSON object, UTF-8, exactly as the addon recorded it |
| Your answer | Any `2xx` status means delivered. The response body is ignored. Redirects are not followed. |

The companion waits 5 seconds for your answer and tries twice. What
happens after a failure, and when events are sent, is described in the
README under
[Sending events to your own tool](../README.md#sending-events-to-your-own-tool).

Things to allow for in a receiver:

- **Duplicates.** An event can arrive more than once. Identify it by
  `char`, `surname`, `realm` and `seq` together.
- **Gaps.** Not every `seq` arrives: only the newest 10 events
  (`max_backlog`) of a character the companion sees for the first time
  are sent, and next to a Discord webhook forwarding is best effort.
- **Order.** Events of one character normally arrive in `seq` order, but
  an event that failed once can arrive after newer ones. Sort by `seq` if
  order matters.
- **Unknown things.** Later versions may add event types and add fields
  to existing ones. Ignore a `type` or a field you don't know. A field is
  only removed, renamed or changed in meaning in a release whose notes
  say "BREAKING CHANGE".
- **Key order.** Don't rely on the order of keys in the object. The
  examples below are laid out for reading; the real body is one line.
- **Language.** Names of items, zones, quests, skills and bosses are in
  the player's game language. Ids (`item_id`, `quest_id`,
  `encounter_id`) and `class` are the same in every language.

## The event

Every event has the same outer fields. `data` holds the details and its
shape depends on `type`.

```json
{
  "seq": 42,
  "ts": 1759300000,
  "char": "Flano",
  "surname": "Wren",
  "realm": "Whatever",
  "class": "WARRIOR",
  "level": 20,
  "played": 408475,
  "type": "level_up",
  "data": { "level": 20 }
}
```

| Field | Type | Present | Meaning |
|---|---|---|---|
| `seq` | integer | always | Counter per character. Rises by one with each recorded event and is never reused. It starts again at 1 only if the addon's saved data is deleted; compare `ts` if you need to notice that. |
| `ts` | integer | always | When the event happened: Unix time in seconds, from the game client's clock. |
| `char` | string | always | The character's first name. |
| `surname` | string | when the character has one | The character's surname. Only the full name (`char` + space + `surname`) is unique on a realm. |
| `realm` | string | always | The realm's name as the game shows it. It can contain spaces. |
| `class` | string | always | The class in capitals, the same in every language: `WARRIOR`, `PALADIN`, `HUNTER`, `ROGUE`, `PRIEST`, `SHAMAN`, `MAGE`, `WARLOCK`, `DRUID`. |
| `level` | integer | always | The character's level when the event happened. On a `level_up` it is the new level. |
| `played` | integer | only with `/doink played on` | Total time played on this character, in seconds. Missing until the game has answered the addon's request, so the first event after turning it on may not have it. |
| `group` | array of strings | only with `/doink group on`, in a party | Who else was in the party, each as `"Name Surname (Class Level)"` (the surname only when the client gives one), at most 4. Left out when the character was alone, in a raid, or when the game hid a name (party names shouldn't be hidden, but every value is checked). Discord doesn't show it. |
| `type` | string | always | What happened. One of the types below. |
| `data` | object | always | The details. See the section for the `type`. |
| `test` | `true` | only on test events | The event was made with `/doink test` and nothing really happened. Never `false`: the field is left out instead. |

Every field listed for a `data` object below is always present. Where a
value can be unknown the field is `null`, not left out. All numbers are
whole numbers.

## Event types

| `type` | Recorded when |
|---|---|
| [`level_up`](#level_up) | The character gains a level |
| [`loot`](#loot) | The character loots a notable item |
| [`death`](#death) | The character dies |
| [`quest`](#quest) | The character turns in a quest |
| [`boss_kill`](#boss_kill) | A dungeon or raid boss dies |
| [`skill_up`](#skill_up) | A profession or weapon skill goes up |
| [`mount`](#mount) | The character gets a new mount |
| [`duel`](#duel) | A duel ends |
| [`battleground`](#battleground) | A battleground ends |
| [`session`](#session) | The character logs in or out (never posted to Discord) |
| [`pvp_kill`](#pvp_kill) | The character lands the killing blow on another player |

A type is only recorded if the player turned it on, and each has filters
the player can change (for example "rare items and better"), so which
events you receive depends on their settings. See
[Notifiers](../README.md#notifiers-what-gets-recorded-for-discord) in the
README.

### `level_up`

```json
{
  "seq": 42, "ts": 1759300000, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 20,
  "type": "level_up",
  "data": { "level": 20 }
}
```

| Field | Type | Meaning |
|---|---|---|
| `level` | integer | The level just reached. |

### `loot`

```json
{
  "seq": 43, "ts": 1759300100, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 60,
  "type": "loot",
  "data": {
    "item_id": 19019,
    "link": "|cnIQ5:|Hitem:19019::::::::|h[Thunderfury, Blessed Blade of the Windseeker]|h|r",
    "name": "Thunderfury, Blessed Blade of the Windseeker",
    "quality": 5,
    "qty": 1,
    "vendor_value": 255355,
    "boe": true,
    "gear": true
  }
}
```

| Field | Type | Meaning |
|---|---|---|
| `item_id` | integer | The game's item id. `https://www.wowhead.com/forever/item=<item_id>` is the item's Wowhead page. |
| `link` | string | The item link exactly as the game wrote it in chat, with its `\|` codes. Use `name` for display. |
| `name` | string | The item's name. |
| `quality` | integer | 0 poor, 1 common, 2 uncommon, 3 rare, 4 epic, 5 legendary. |
| `qty` | integer | How many were looted at once. |
| `vendor_value` | integer | What a vendor pays for the whole stack (`qty` items), in copper: 100 copper = 1 silver, 10000 = 1 gold. 0 when the item can't be sold. |
| `boe` | boolean or `null` | Whether the item binds when equipped. `null` when the game didn't say. |
| `gear` | boolean or `null` | Whether the item is a weapon or armour. `null` when the game didn't say. |

Only items the character looted themselves are recorded: not quest
rewards, vendor purchases or crafted items. Loot during a boss fight is
not seen, because the game hides chat from addons until the fight ends.

### `death`

```json
{
  "seq": 44, "ts": 1759300200, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 20,
  "type": "death",
  "data": {
    "zone": "Westfall",
    "subzone": "Moonbrook",
    "killer": "Hogger",
    "killer_kind": "npc",
    "environment": null
  }
}
```

| Field | Type | Meaning |
|---|---|---|
| `zone` | string | The zone the character died in. |
| `subzone` | string or `null` | The area within the zone. `null` where the game names none. |
| `killer` | string or `null` | Who landed the last hit: a creature's or a player's name. `null` for an environmental death, or when the game gave no cause. |
| `killer_kind` | string or `null` | What the killer was: `player`, `pet` (a player's pet; whose isn't known) or `npc`, from its GUID in the death recap. Some player summons (totems, guardians) may read as `npc`. `null` when the game gave no GUID or hid it, and when there was no killer. A creature can share a player's name, so `player` is the reliable sign of a PvP death. Added after 0.19.0: older addons leave it out. |
| `environment` | string or `null` | For an environmental death, its kind in capitals: `FALLING`, `DROWNING`, `FATIGUE`, `FIRE`, `LAVA` or `SLIME`. The value comes from the game, so allow for others. `null` otherwise. |

At most one of `killer` and `environment` is set. Both are `null` when
the cause is unknown.

### `quest`

```json
{
  "seq": 45, "ts": 1759300300, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 20,
  "type": "quest",
  "data": { "quest_id": 783, "title": "A Threat Within", "xp": 40 }
}
```

| Field | Type | Meaning |
|---|---|---|
| `quest_id` | integer | The game's quest id. `https://www.wowhead.com/forever/quest=<quest_id>` is the quest's Wowhead page. |
| `title` | string | The quest's title. `Quest #<quest_id>` when the game couldn't give one. |
| `xp` | integer | Experience rewarded. 0 when there was none. |

### `boss_kill`

```json
{
  "seq": 46, "ts": 1759300400, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 60,
  "type": "boss_kill",
  "data": {
    "encounter_id": 672,
    "name": "Ragnaros",
    "instance": "Molten Core",
    "difficulty": 9,
    "success": true,
    "group_size": 40,
    "first_kill": true,
    "group": ["Grakkor Highmountain (Shaman 60)"]
  }
}
```

| Field | Type | Meaning |
|---|---|---|
| `encounter_id` | integer | The game's encounter id. |
| `name` | string | The boss encounter's name. |
| `instance` | string or `null` | The dungeon or raid. `null` when the game didn't say. |
| `difficulty` | integer | The game's difficulty id for the instance. |
| `success` | `true` | Always `true`: wipes are not recorded. |
| `group_size` | integer | The group size the game reports for the encounter, for example 5 or 40. |
| `first_kill` | boolean | `true` the first time this character kills this boss, as far as the addon's saved data remembers. |
| `group` | array of strings, or `null` | The other party members (at most 4), each as `Name Surname (Class Level)`. `null` in a raid, when alone, or when the game hides the names. |

### `skill_up`

```json
{
  "seq": 47, "ts": 1759300500, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 30,
  "type": "skill_up",
  "data": { "skill": "Blacksmithing", "rank": 150, "max_rank": 150 }
}
```

| Field | Type | Meaning |
|---|---|---|
| `skill` | string | The skill's name. |
| `rank` | integer | The rank just reached. |
| `max_rank` | integer or `null` | The current cap for this skill. `null` when the game offers none, which is always the case for weapon skills. |

Skill-ups during a boss fight are not seen, for the same reason as loot.

### `mount`

```json
{
  "seq": 48, "ts": 1759300600, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 40,
  "type": "mount",
  "data": {
    "name": "Gray Wolf",
    "item_id": 5656,
    "spell_id": null,
    "link": "|cnIQ3:|Hitem:5656::::::::|h[Gray Wolf]|h|r",
    "bought": true
  }
}
```

| Field | Type | Meaning |
|---|---|---|
| `name` | string or `null` | The mount's name. |
| `item_id` | integer or `null` | The mount item's id, when the mount was seen as an item arriving in the bags. |
| `spell_id` | integer or `null` | The mount's spell id, when it was seen through the mount journal instead. |
| `link` | string or `null` | The item link as the game wrote it, when there is an item. |
| `bought` | boolean | `true` when a vendor window was open at the time. |

Normally either `item_id` and `link` are set, or `spell_id` is. At most
one mount is recorded per two minutes.

### `duel`

```json
{
  "seq": 49, "ts": 1759300700, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 20,
  "type": "duel",
  "data": {
    "opponent": "Grakkor Highmountain",
    "won": true,
    "fled": false,
    "makgora": false
  }
}
```

| Field | Type | Meaning |
|---|---|---|
| `opponent` | string | The other player's name as the game's message gave it. |
| `won` | boolean | Whether this character won. |
| `fled` | boolean | `true` when the duel ended because the loser left the duel area. |
| `makgora` | boolean | `true` for a Mak'gora, the duel to the death. |

By default only wins are recorded.

### `battleground`

```json
{
  "seq": 50, "ts": 1759300800, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 20,
  "type": "battleground",
  "data": { "name": "Warsong Gulch", "won": true }
}
```

| Field | Type | Meaning |
|---|---|---|
| `name` | string | The battleground's name. |
| `won` | boolean | Whether this character's faction won. |

By default only wins are recorded.

### `session`

```json
{
  "seq": 51, "ts": 1759301000, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 20,
  "type": "session",
  "data": { "state": "login" }
}
```

| Field | Type | Meaning |
|---|---|---|
| `state` | string | `login`: entered the world from character select. `reload`: the UI was reloaded. `logout`: logged out, or the UI is about to reload. |

Logins and logouts exist for tools like yours: the companion forwards them
but never posts them to Discord, and the addon never sends them as a
realtime screenshot. They reach you with the game's next save, at
`/reload` or logout, so a session's `login` and `logout` usually arrive
together, seconds after the logout.

- A `/reload` records `logout` and then `reload` within a second or two.
  A `logout` not followed by a `reload` is a real logout.
- `played` (with `/doink played on`) is usually missing on `login` and
  `reload`, because the game hasn't answered the addon's request yet; it
  is there on `logout` once the game has answered. Without it, the session's length is the logout's
  `ts` minus the login's `ts`.
- A crash or Alt-F4 saves nothing, so that session's events never arrive.

### `pvp_kill`

```json
{
  "seq": 52, "ts": 1759301100, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 20,
  "type": "pvp_kill",
  "data": {
    "victim": "Grakkor",
    "class": "SHAMAN",
    "race": "Orc",
    "level": 24,
    "zone": "Westfall",
    "subzone": "Moonbrook"
  }
}
```

| Field | Type | Meaning |
|---|---|---|
| `victim` | string or `null` | The player killed, by first name. `null` when the game had no details on them. |
| `class` | string or `null` | Their class in capitals, the same in every language. |
| `race` | string or `null` | Their race in English. |
| `level` | integer or `null` | Their level. |
| `zone` | string | Where it happened. |
| `subzone` | string or `null` | The area within the zone. `null` where the game names none. |

Only killing blows count, not every kill you helped with. PvP kills are posted to Discord but never announced in chat.

## Test events

`/doink test <type>` in game makes a fake event that goes through the
whole pipeline, so you can try a receiver without waiting for the real
thing. It has a real `seq` and the extra field `"test": true`:

```json
{
  "seq": 51, "ts": 1759300900, "char": "Flano", "surname": "Wren",
  "realm": "Whatever", "class": "WARRIOR", "level": 20,
  "type": "quest",
  "data": { "quest_id": 783, "title": "A Threat Within", "xp": 40 },
  "test": true
}
```

The `data` of a test event is the example shown for its type above, with
two exceptions: a test `level_up` is the character's current level plus
one, and a test `death` uses the zone the character is standing in.

The type must be turned on for the test to be recorded
(`/doink enable <type>`). The companion's own `doink test` command posts
to Discord only and forwards nothing.

## Where the events live on disk

The addon keeps the newest 500 events per character in its saved
variables file,
`<WoW>/<flavor>/WTF/Account/<ACCOUNT>/SavedVariables/DOINK.lua`, each as
one JSON string in a Lua table. The game writes that file only at
`/reload` and at logout. The README describes the file under
[How it works: what the addon stores](../README.md#how-it-works-what-the-addon-stores).
Reading it yourself works, but `forward_url` is the supported way to get
events.
