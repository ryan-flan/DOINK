-- Offline test harness: stubs just enough of the WoW API to load the addon
-- and drive it with real-format events. Lua 5.1 / LuaJIT.
--   cd addon && luajit tests/test_addon.lua

local BASE = (arg and arg[0] or ""):match("^(.*)/tests/") or "."
local ROOT = BASE .. "/DOINK"
local FIXTURES = BASE .. "/tests/fixtures"

------------------------------------------------------------------ WoW stubs

local frames = {}
function CreateFrame(_, _, parent)
  local f = { events = {}, shown = false, textures = {}, parent = parent }
  function f:RegisterEvent(e) self.events[e] = true end
  function f:UnregisterEvent(e) self.events[e] = nil end
  function f:SetScript(kind, fn)
    if kind == "OnUpdate" then self.onUpdate = fn else self.onEvent = fn end
  end
  function f:SetFrameStrata(s) self.strata = s end
  function f:SetFrameLevel(l) self.level = l end
  function f:EnableMouse(on) self.mouse = on end
  function f:SetIgnoreParentScale(on) self.ignoreParentScale = on end
  function f:SetScale(s) self.scale = s end
  function f:SetSize(w, h) self.w, self.h = w, h end
  function f:ClearAllPoints() self.point = nil end
  function f:SetPoint(p, rel, rp, x, y) self.point = { p, rel, rp, x, y } end
  function f:Show() self.shown = true end
  function f:Hide() self.shown = false end
  function f:IsShown() return self.shown end
  function f:IsVisible() return self.shown and (not self.parent or self.parent:IsVisible()) end
  function f:GetEffectiveScale() return 1 end
  function f:CreateTexture()
    local t = {}
    function t:SetSize(w, h) self.w, self.h = w, h end
    function t:SetPoint(_, _, _, x, y) self.x, self.y = x, y end
    function t:SetColorTexture(r) self.color = r end
    function t:Show() self.hidden = false end
    function t:Hide() self.hidden = true end
    f.textures[#f.textures + 1] = t
    return t
  end
  frames[#frames + 1] = f
  return f
end
WorldFrame = CreateFrame("Frame")
UIParent = CreateFrame("Frame")
UIParent:Show()
function GetPhysicalScreenSize() return 1920, 1080 end

-- Screenshots and the CVars the screenshot transport swaps.
local screenshots = 0
function Screenshot() screenshots = screenshots + 1 end
local cvars = { screenshotFormat = "jpeg", screenshotQuality = "3" }
C_CVar = {
  GetCVar = function(name) return cvars[name] end,
  SetCVar = function(name, value) cvars[name] = tostring(value) end,
}

-- Chat and timers, for Announce.lua.
local chat = {}          -- every SendChatMessage call: { line, chatType, target }
local inGuild, inGroup, inRaid = false, false, false
function SendChatMessage(msg, chatType, _, target) chat[#chat + 1] = { msg, chatType, target } end
function IsInGuild() return inGuild end
function IsInGroup() return inGroup end
function IsInRaid() return inRaid end
function GetUnitName() return "Flano Wren" end
local timers = {}
C_Timer = { After = function(seconds, fn) timers[#timers + 1] = { seconds, fn } end }
local function runTimers() -- run everything scheduled, including what that schedules
  while #timers > 0 do
    local t = table.remove(timers, 1)
    t[2]()
  end
end

local function fire(event, ...)
  for _, f in ipairs(frames) do
    if f.events[event] then f.onEvent(f, event, ...) end
  end
end

-- Forever beta strings (no trailing periods on loot; see CLAUDE.md).
LOOT_ITEM_SELF = "You receive loot: %s"
LOOT_ITEM_SELF_MULTIPLE = "You receive loot: %sx%d"
SKILL_RANK_UP = "Your skill in %s has increased to %d."
LOOT_ITEM_PUSHED_SELF = "You receive item: %s"
LOOT_ITEM_PUSHED_SELF_MULTIPLE = "You receive item: %sx%d"
DUEL_WINNER_KNOCKOUT = "%1$s has defeated %2$s in a duel"
DUEL_WINNER_RETREAT = "%2$s has fled from %1$s in a duel"
MAKGORA_WINNER_TEST = "%1$s has slain %2$s in a Mak'gora" -- stand-in; real names unknown until launch

-- /played: RequestTimePlayed() is answered with TIME_PLAYED_MSG, which the
-- chat frames print unless they stopped listening.
local playedRequests, playedLines = 0, 0
function RequestTimePlayed() playedRequests = playedRequests + 1 end
NUM_CHAT_WINDOWS = 2
for i = 1, NUM_CHAT_WINDOWS do
  local chatFrame = CreateFrame("Frame")
  function chatFrame:IsEventRegistered(e) return self.events[e] == true end
  chatFrame:RegisterEvent("TIME_PLAYED_MSG")
  chatFrame:SetScript("OnEvent", function() playedLines = playedLines + 1 end)
  _G["ChatFrame" .. i] = chatFrame
end

local now, level = 1000, 9
time = function() return 1790870000 end
function GetTime() return now end

-- A real death: a minute passes first, because the notifier ignores a
-- second PLAYER_DEAD within its cooldown (feign death fires it too).
local function die()
  now = now + 60
  fire("PLAYER_DEAD")
end
-- Party members for boss kills; the player stubs stay as they were.
local party = {
  party1 = { "Grakkor", "Highmountain", "Shaman", "SHAMAN", 24 },
  party2 = { "Mira", nil, "Mage", "MAGE", 22 },
}
local partyOn = false
function UnitExists(unit)
  if unit == "player" then return true end
  return partyOn and party[unit] ~= nil
end
function UnitName(unit)
  local p = unit and party[unit]
  if p then return p[1], p[2] end
  return "Flano", "Wren" -- Forever: surname second
end
function UnitGUID() return "Player-1-ABC" end
function GetRealmName() return "Classic Beta PvE 2" end
function UnitClass(unit)
  local p = unit and party[unit]
  if p then return p[3], p[4] end
  return "Warrior", "WARRIOR"
end
function UnitLevel(unit)
  local p = unit and party[unit]
  if p then return p[5] end
  return level
end
local faction, instanceKind, bgWinner = "Alliance", "none", nil
function UnitFactionGroup() return faction end
function IsInInstance() return instanceKind ~= "none", instanceKind end
function GetBattlefieldWinner() return bgWinner end
C_MountJournal = nil
function GetZoneText() return "Elwynn Forest" end
function GetSubZoneText() return "" end
function GetInstanceInfo() return "Deadmines", "party" end
function GetTitleText() return "Wolves Across the Border" end
function GetAddOnMetadata() return "test" end
function ReloadUI() end
function geterrorhandler() return function(e) error(e, 0) end end
SlashCmdList = {}

-- name, link, quality, ilvl, reqLevel, class, subclass, maxStack, equipLoc,
-- texture, sellPrice, classID, subclassID, bindType
local itemCache = {
  [769] = { "Chunk of Boar Meat", nil, 1, 0, 0, nil, nil, 20, "", 0, 4, nil, nil, 0 },
  [2079] = { "Sergeant's Blade", nil, 2, 0, 0, nil, nil, 1, "", 0, 100, nil, nil, 2 },
  [2140] = { "Carving Knife", nil, 3, 0, 0, nil, nil, 1, "", 0, 260, nil, nil, 2 },
  [4875] = { "Broken Boar Tusk", nil, 0, 0, 0, nil, nil, 1, "", 0, 20000, nil, nil, 0 },
  [6314] = { "Wolfmaster Cape", nil, 3, 0, 0, nil, nil, 1, "", 0, 1215, nil, nil, 1 },
  [13033] = { "Zealot Blade", nil, 3, 0, 0, nil, nil, 1, "", 0, 9000, nil, nil, 2 },
  [17182] = { "Sulfuras", nil, 5, 0, 0, nil, nil, 1, "", 0, 0, nil, nil, 1 },
  [17010] = { "Elementium Bar", nil, 5, 0, 0, nil, nil, 20, "", 0, 0, nil, nil, 0 },
  -- No 14th value: a client that doesn't report bindType.
  [9999] = { "Mystery Rare", nil, 3, 0, 0, nil, nil, 1, "", 0, 500, nil, nil, nil },
}
local uncached = {}
function GetItemInfo(id)
  if uncached[id] then return nil end
  local row = itemCache[id]
  if row then return unpack(row, 1, 14) end
end
-- classID, subclassID as GetItemInfoInstant's 6th and 7th values.
local itemClass = {
  [13033] = { 2, 7 }, [6314] = { 4, 1 }, [17182] = { 2, 5 }, [17010] = { 7, 0 }, [5656] = { 15, 5 },
}
function GetItemInfoInstant(linkOrID)
  local id = tonumber(linkOrID) or tonumber(tostring(linkOrID):match("|Hitem:(%d+)"))
  local c = itemClass[id]
  if not c then return id end
  return id, nil, nil, nil, nil, c[1], c[2]
end

PROTECTED_EVENTS = { COMBAT_LOG_EVENT_UNFILTERED = true }

local skills = {
  { "Professions", true }, { "Blacksmithing", false, nil, 150, 0, 0, 225 },
}
function GetNumSkillLines() return #skills end
function GetSkillLineInfo(i) return unpack(skills[i], 1, 7) end

-- The client's Settings API, for Options.lua: records every control.
local settings = { controls = {}, opened = nil, categories = 0 }
Settings = {
  VarType = { Boolean = "boolean", Number = "number", String = "string" },
  RegisterVerticalLayoutCategory = function(name)
    settings.categories = settings.categories + 1
    local layout = { headers = {} }
    function layout:AddInitializer(init) self.headers[#self.headers + 1] = init end
    return { name = name, GetID = function() return 42 end }, layout
  end,
  RegisterAddOnCategory = function(category) settings.registered = category end,
  RegisterProxySetting = function(_, variable, varType, name, default, get, set)
    -- The real client rejects a default whose Lua type doesn't match varType.
    assert(type(default) == varType, variable .. ": default is a " .. type(default) .. ", declared " .. varType)
    assert(type(get()) == varType, variable .. ": getter returns a " .. type(get()) .. ", declared " .. varType)
    return { variable = variable, varType = varType, name = name, default = default, get = get, set = set }
  end,
  CreateCheckbox = function(_, setting) setting.kind = "checkbox"; settings.controls[#settings.controls + 1] = setting end,
  CreateDropdown = function(_, setting, options)
    setting.kind = "dropdown"; setting.options = options()
    settings.controls[#settings.controls + 1] = setting
  end,
  CreateSlider = function(_, setting) setting.kind = "slider"; settings.controls[#settings.controls + 1] = setting end,
  CreateControlTextContainer = function()
    local c = { data = {} }
    function c:Add(value, label) self.data[#self.data + 1] = { value = value, label = label } end
    function c:GetData() return self.data end
    return c
  end,
  CreateSliderOptions = function(min, max, step)
    return { min = min, max = max, step = step, SetLabelFormatter = function() end }
  end,
  OpenToCategory = function(id) settings.opened = id end,
}
function CreateSettingsListSectionHeaderInitializer(title) return title end
MinimalSliderWithSteppersMixin = { Label = { Right = 1 } }

local printed = {}
print = function(s) printed[#printed + 1] = s end

------------------------------------------------------------------ load

local ns = {}
for _, file in ipairs({ "Json.lua", "Defaults.lua", "Core.lua",
    "Notifiers/LevelUp.lua", "Notifiers/Loot.lua", "Notifiers/Death.lua",
    "Notifiers/Quest.lua", "Notifiers/BossKill.lua", "Notifiers/SkillUp.lua",
    "Notifiers/Mount.lua", "Notifiers/Duel.lua", "Notifiers/Battleground.lua",
    "Notifiers/Session.lua", "Notifiers/PvpKill.lua",
    "Announce.lua", "Transports/Screenshot.lua", "Options.lua" }) do
  assert(loadfile(ROOT .. "/" .. file))("DOINK", ns)
end

-- SavedVariables as written by v0.2.0, before surnames were known.
local OLD_KEY, NEW_KEY = "Flano-Classic Beta PvE 2", "Flano Wren-Classic Beta PvE 2"
local OLD_HOOK = "https://discord.com/api/webhooks/9/old"
DOINKDB = {
  version = 1,
  realtime = { enabled = false }, -- a v0.16 table: no `image` key yet
  webhooks = { [OLD_KEY] = OLD_HOOK },
  chars = {
    [OLD_KEY] = {
      seq = 2,
      config = { quest = { enabled = true } },
      events = {
        '{"char":"Flano","realm":"Classic Beta PvE 2","seq":1,"type":"level_up"}',
        '{"char":"Flano","realm":"Classic Beta PvE 2","seq":2,"type":"quest"}',
      },
    },
  },
}
fire("ADDON_LOADED", "DOINK")
fire("PLAYER_LOGIN")

-- Shipped defaults: everything off. Remember them for one test, then give the
-- suite the "everything on" rules the older tests were written against.
local SHIPPED = {}
for t, d in pairs(ns.Defaults) do
  if d.enabled ~= nil then SHIPPED[t] = d.enabled end
  if t ~= "announce" and t ~= "general" then d.enabled = true end
end
local SHIPPED_ANNOUNCE = {}
for k, v in pairs(ns.Defaults.announce) do SHIPPED_ANNOUNCE[k] = v end
local TEST_ANNOUNCE = { channel = "guild", level_up = "milestones", loot = "epic", death = true,
  boss_kill = true, mount = true, duel = true, battleground = true }
for k, v in pairs(TEST_ANNOUNCE) do ns.Defaults.announce[k] = v end

------------------------------------------------------------------ helpers

local passed, failed = 0, 0
local function test(name, fn)
  local ok, err = pcall(fn)
  if ok then passed = passed + 1 else
    failed = failed + 1
    io.stderr:write("FAIL " .. name .. ": " .. tostring(err) .. "\n")
  end
end

local function eq(a, b, what)
  if a ~= b then error(string.format("%s: expected %s, got %s", what or "value", tostring(b), tostring(a)), 2) end
end

local events = function() return ns.char.events end
local function count() return #events() end
local function last() return events()[#events()] end
local function has(s) return last():find(s, 1, true) ~= nil end

local function link(id, name, q)
  return string.format("|cnIQ%d:|Hitem:%d::::::::8:1491::::::::::|h[%s]|h|r", q, id, name)
end

------------------------------------------------------------------ tests

test("surname: pre-surname entry migrates to the full-name key", function()
  eq(DOINKDB.chars[OLD_KEY], nil, "old key gone")
  local char = DOINKDB.chars[NEW_KEY]
  assert(char, "new key exists")
  eq(char.seq, 2, "seq carried over")
  eq(char.config.quest.enabled, true, "settings carried over")
  eq(DOINKDB.webhooks[NEW_KEY], OLD_HOOK, "per-char webhook carried over")
  eq(DOINKDB.webhooks[OLD_KEY], nil, "old webhook key gone")
  -- Queued events gain the surname and stay otherwise intact.
  eq(char.events[1], '{"surname":"Wren","char":"Flano","realm":"Classic Beta PvE 2","seq":1,"type":"level_up"}', "event 1")
  assert(char.events[2]:find('^{"surname":"Wren","char":"Flano"'), char.events[2])
  DOINKDB.webhooks[NEW_KEY] = nil -- keep the webhook tests below independent
end)

test("surname: new events carry it, seq continues", function()
  SlashCmdList.DOINK("test levelup")
  assert(has('"char":"Flano"') and has('"surname":"Wren"') and has('"seq":3,'), last())
end)

test("FormatToPattern escapes magic and captures", function()
  local p = ns.FormatToPattern("You receive loot: %sx%d.")
  local l, q = ("You receive loot: [a.b]x3."):match(p)
  eq(l, "[a.b]", "link"); eq(q, "3", "qty")
  eq(("You receive loot: [a]x3!"):match(p), nil, "trailing char must not match")
end)

test("loot: real beta message, below threshold, is ignored", function()
  local n = count()
  fire("CHAT_MSG_LOOT", "You receive loot: " .. link(769, "Chunk of Boar Meat", 1), "Flano")
  eq(count(), n, "events")
end)

test("loot: rare item is emitted", function()
  local n = count()
  fire("CHAT_MSG_LOOT", "You receive loot: " .. link(2140, "Carving Knife", 3))
  eq(count(), n + 1, "events")
  assert(has('"item_id":2140') and has('"quality":3') and has('"qty":1'), last())
  assert(has('"type":"loot"') and has('"vendor_value":260'), last())
end)

test("loot: stack, vendor value threshold counts the whole stack", function()
  local n = count()
  fire("CHAT_MSG_LOOT", "You receive loot: " .. link(4875, "Broken Boar Tusk", 0) .. "x2")
  eq(count(), n + 1, "events")
  assert(has('"qty":2') and has('"vendor_value":40000'), last())
  assert(has('"name":"Broken Boar Tusk"'), last())
end)

test("loot: someone else's loot is ignored", function()
  local n = count()
  fire("CHAT_MSG_LOOT", "Bob receives loot: " .. link(2140, "Carving Knife", 3), "Bob")
  eq(count(), n, "events")
end)

test("loot: uncached item waits for GET_ITEM_INFO_RECEIVED", function()
  local n = count()
  uncached[2140] = true
  fire("CHAT_MSG_LOOT", "You receive loot: " .. link(2140, "Carving Knife", 3))
  eq(count(), n, "before info")
  uncached[2140] = nil
  fire("GET_ITEM_INFO_RECEIVED", 999, true)
  eq(count(), n, "unrelated item")
  fire("GET_ITEM_INFO_RECEIVED", 2140, true)
  eq(count(), n + 1, "after info")
  fire("GET_ITEM_INFO_RECEIVED", 2140, true)
  eq(count(), n + 1, "no double emit")
end)

test("secret values (encounter restrictions) never take dispatch down", function()
  -- The real client hands addons unreadable "secret" arguments during an
  -- encounter; tostring/concat on them throws. Stand-in: a marked table.
  local secret = setmetatable({}, { __tostring = function() error("secret") end })
  issecretvalue = function(v) return v == secret end
  local n = count()
  DOINKDB.debug = true
  fire("CHAT_MSG_LOOT", secret, secret, "", "", secret)
  fire("CHAT_MSG_SKILL", secret)
  DOINKDB.debug = false
  fire("CHAT_MSG_LOOT", secret)
  eq(count(), n, "nothing emitted from a secret message")
  issecretvalue = nil
  -- And a plain loot line still works right after.
  fire("CHAT_MSG_LOOT", "You receive loot: " .. link(2140, "Carving Knife", 3))
  eq(count(), n + 1, "normal loot after the encounter")
end)

test("no notifier registers events protected in Forever", function()
  -- Registering these pops "DOINK has been blocked from an action only
  -- available to the Blizzard UI" at load. See CLAUDE.md beta facts.
  for _, f in ipairs(frames) do
    for event in pairs(PROTECTED_EVENTS) do
      assert(not f.events[event], event .. " is registered")
    end
  end
end)

test("death: zone and subzone, killer unknown without a death recap", function()
  DeathRecap_HasEvents, DeathRecap_GetEvents = nil, nil
  local n = count()
  die()
  eq(count(), n + 1, "emitted at once, no timer")
  assert(has('"type":"death"') and has('"zone":"Elwynn Forest"'), last())
  assert(has('"subzone":null') and has('"killer":null') and has('"environment":null'), last())
end)

test("death: feign death and a second PLAYER_DEAD within 10 s are ignored", function()
  -- A hunter's saved file held two deaths one second apart (2026-10-03).
  DeathRecap_HasEvents, DeathRecap_GetEvents = nil, nil
  local n = count()
  die()
  eq(count(), n + 1, "first death")
  now = now + 1
  fire("PLAYER_DEAD")
  eq(count(), n + 1, "the one-second-later duplicate is dropped")
  now = now + 9
  fire("PLAYER_DEAD")
  eq(count(), n + 2, "ten seconds on it's a new death")

  UnitIsFeignDeath = function() return true end
  now = now + 60
  fire("PLAYER_DEAD")
  eq(count(), n + 2, "feign death is not a death")
  UnitIsFeignDeath = nil

  -- A pending recap retry also blocks a duplicate, and emits once.
  local recap = {}
  DeathRecap_HasEvents = function() return #recap > 0 end
  DeathRecap_GetEvents = function() return recap end
  timers = {}
  die()
  fire("PLAYER_DEAD")
  eq(#timers, 1, "one retry, not two")
  recap = { { sourceName = "Son of Arugal" } }
  runTimers()
  eq(count(), n + 3, "one event from the retry")
  DeathRecap_HasEvents, DeathRecap_GetEvents = nil, nil
end)

test("death: killer is the newest death recap hit", function()
  local recap = {}
  DeathRecap_HasEvents = function() return #recap > 0 end
  DeathRecap_GetEvents = function() return recap end
  recap = { { sourceName = "Kobold Vermin", amount = 3, timestamp = 5 },
            { sourceName = "Hogger", amount = 40, timestamp = 7 },
            { sourceName = "Kobold Vermin", amount = 2, timestamp = 6 } }
  die()
  assert(has('"killer":"Hogger"'), last())

  recap = { { environmentalType = "Falling", amount = 999 } }
  die()
  assert(has('"killer":null') and has('"environment":"FALLING"'), last())

  -- Recap empty at PLAYER_DEAD, filled shortly after: one 0.5 s retry.
  recap = {}
  local n = count()
  timers = {}
  die()
  eq(count(), n, "waits for the recap")
  eq(#timers, 1, "one retry scheduled"); eq(timers[1][1], 0.5, "retry delay")
  recap = { { sourceName = "Defias Pillager" } }
  runTimers()
  eq(count(), n + 1, "emitted after the retry")
  assert(has('"killer":"Defias Pillager"'), last())

  -- Still empty after the retry: emit anyway with killer null.
  recap = {}
  die(); runTimers()
  assert(has('"killer":null'), last())
  DeathRecap_HasEvents, DeathRecap_GetEvents = nil, nil
end)

test("death: C_DeathRecap first; without timestamps the first entry is the killing blow", function()
  -- Blizzard's recap marks entry 1 as the hit that caused the death.
  local recap = { { sourceName = "Hogger" }, { sourceName = "Kobold Vermin" } }
  DeathRecap_HasEvents = function() return true end
  DeathRecap_GetEvents = function() return { { sourceName = "Old Alias" } } end
  C_DeathRecap = {
    HasRecapEvents = function() return #recap > 0 end,
    GetRecapEvents = function() return recap end,
  }
  die()
  assert(has('"killer":"Hogger"'), last())
  C_DeathRecap = nil
  die()
  assert(has('"killer":"Old Alias"'), "the aliases still work alone: " .. last())
  DeathRecap_HasEvents, DeathRecap_GetEvents = nil, nil
end)

test("death: a recap the client hides (secret values) still records the death, killer unknown", function()
  local recap = { { sourceName = "Hidden Harry", timestamp = "secret-ts" },
                  { sourceName = "Kobold Vermin", timestamp = 4 } }
  C_DeathRecap = {
    HasRecapEvents = function() return true end,
    GetRecapEvents = function() return recap end,
  }
  issecretvalue = function(v) return v == "Hidden Harry" or v == "secret-ts" end
  local n = count()
  timers = {}
  die(); runTimers()
  eq(count(), n + 1, "the death is still recorded")
  assert(has('"killer":"Kobold Vermin"'), "a secret timestamp is skipped: " .. last())
  recap = { { sourceName = "Hidden Harry" } }
  die(); runTimers()
  eq(count(), n + 2, "recorded")
  assert(has('"killer":null'), "a secret name counts as unknown: " .. last())
  issecretvalue = nil
  C_DeathRecap = nil
end)

test("death: killer_kind from the killing blow's GUID; null when unknown or secret", function()
  local recap = {}
  C_DeathRecap = {
    HasRecapEvents = function() return #recap > 0 end,
    GetRecapEvents = function() return recap end,
  }
  recap = { { sourceName = "Grakkor", sourceGUID = "Player-5826-0A1B2C3D" } }
  die()
  assert(has('"killer":"Grakkor"') and has('"killer_kind":"player"'), last())
  recap = { { sourceName = "Wolf", sourceGUID = "Pet-0-5826-0-1234-1-0100A1B2C3" } }
  die()
  assert(has('"killer_kind":"pet"'), last())
  recap = { { sourceName = "Hogger", sourceGUID = "Creature-0-5826-0-1234-448-0000A1B2C3" } }
  die()
  assert(has('"killer_kind":"npc"'), last())
  recap = { { sourceName = "Hogger" } }
  die()
  assert(has('"killer_kind":null'), "no GUID: " .. last())
  issecretvalue = function(v) return type(v) == "string" and v:find("^Player") ~= nil end
  recap = { { sourceName = "Grakkor", sourceGUID = "Player-5826-0A1B2C3D" } }
  die()
  assert(has('"killer":"Grakkor"') and has('"killer_kind":null'), "secret GUID: " .. last())
  issecretvalue = nil
  recap = { { environmentalType = "Drowning" } }
  die()
  assert(has('"killer_kind":null') and has('"environment":"DROWNING"'), last())
  C_DeathRecap = nil
end)

test("quest: title from reward screen when log lookup fails", function()
  fire("QUEST_COMPLETE")
  fire("QUEST_TURNED_IN", 33, 450, 75)
  assert(has('"quest_id":33') and has('"xp":450') and has('"title":"Wolves Across the Border"'), last())
end)

test("boss_kill: success emits, wipe doesn't", function()
  local n = count()
  fire("ENCOUNTER_END", 1144, "Edwin VanCleef", 1, 5, 0)
  eq(count(), n, "wipe")
  fire("ENCOUNTER_END", 1144, "Edwin VanCleef", 1, 5, 1)
  eq(count(), n + 1, "kill")
  assert(has('"instance":"Deadmines"') and has('"group_size":5') and has('"success":true'), last())
end)

test("skill_up: milestones only by default", function()
  local n = count()
  fire("CHAT_MSG_SKILL", "Your skill in Blacksmithing has increased to 149.")
  eq(count(), n, "non-milestone")
  fire("CHAT_MSG_SKILL", "Your skill in Blacksmithing has increased to 150.")
  eq(count(), n + 1, "milestone")
  assert(has('"skill":"Blacksmithing"') and has('"rank":150') and has('"max_rank":225'), last())
end)

test("skill_up: Forever has no skills panel API (bug reports 2026-10-03)", function()
  local numLines, lineInfo = GetNumSkillLines, GetSkillLineInfo
  GetNumSkillLines, GetSkillLineInfo = nil, nil
  local n = count()
  fire("CHAT_MSG_SKILL", "Your skill in Bows has increased to 225.")
  eq(count(), n + 1, "emitted without the old API")
  assert(has('"skill":"Bows"') and has('"max_rank":null'), last())

  -- The modern engine lists professions instead.
  GetProfessions = function() return 3, 7 end
  GetProfessionInfo = function(i)
    if i == 3 then return "Blacksmithing", 1, 150, 300 end
    return "Mining", 2, 75, 150
  end
  fire("CHAT_MSG_SKILL", "Your skill in Mining has increased to 75.")
  assert(has('"skill":"Mining"') and has('"max_rank":150'), last())
  GetProfessionInfo = function() error("boom") end
  fire("CHAT_MSG_SKILL", "Your skill in Mining has increased to 150.")
  assert(has('"rank":150') and has('"max_rank":null'), "an API error never loses the event")
  GetProfessions, GetProfessionInfo = nil, nil
  GetNumSkillLines, GetSkillLineInfo = numLines, lineInfo
end)

test("level_up: envelope level isn't stale when UnitLevel lags", function()
  -- Beta: during PLAYER_LEVEL_UP, UnitLevel still returns the old level.
  level = 9
  fire("PLAYER_LEVEL_UP", 10)
  assert(has('"data":{"level":10}') and has('"level":10,'), last())
  fire("QUEST_TURNED_IN", 818, 625, 0) -- same-second turn-in that caused it
  assert(has('"level":10,'), last())
end)

local function slash(msg)
  printed = {}
  SlashCmdList.DOINK(msg)
  return table.concat(printed, "\n")
end

test("FormatToPatternOrdered: captures come back in argument order", function()
  local p, order = ns.FormatToPatternOrdered(DUEL_WINNER_KNOCKOUT)
  eq(p, "^(.-) has defeated (.-) in a duel$", "pattern")
  local a = ns.MatchOrdered("Bob has defeated Al in a duel", p, order)
  eq(a[1], "Bob", "winner"); eq(a[2], "Al", "loser")
  p, order = ns.FormatToPatternOrdered(DUEL_WINNER_RETREAT)
  a = ns.MatchOrdered("Al has fled from Bob in a duel", p, order)
  eq(a[1], "Bob", "winner from a swapped-order string"); eq(a[2], "Al", "loser")
  p, order = ns.FormatToPatternOrdered("Got %s (%d).")
  eq(p, "^Got (.+) %((%d+)%)%.$", "plain %s/%d, magic escaped")
  a = ns.MatchOrdered("Got it (3).", p, order)
  eq(a[1], "it", "arg 1"); eq(a[2], "3", "arg 2")
  eq(ns.MatchOrdered("nope", p, order), nil, "no match")
end)

local function lootMsg(id, name, q) return "You receive loot: " .. link(id, name, q) end

test("loot: BoE rare posts, BoP rare doesn't (rares_boe_only)", function()
  slash("reset loot")
  local n = count()
  fire("CHAT_MSG_LOOT", lootMsg(6314, "Wolfmaster Cape", 3))
  eq(count(), n, "BoP rare skipped")
  fire("CHAT_MSG_LOOT", lootMsg(13033, "Zealot Blade", 3))
  eq(count(), n + 1, "BoE rare posted")
  slash("set loot rares_boe_only off")
  fire("CHAT_MSG_LOOT", lootMsg(6314, "Wolfmaster Cape", 3))
  eq(count(), n + 2, "BoP rare posted with rares_boe_only off")
  slash("reset loot")
end)

test("loot: rare with unknown bind type still posts (rares_boe_only)", function()
  slash("reset loot")
  local n = count()
  fire("CHAT_MSG_LOOT", lootMsg(9999, "Mystery Rare", 3))
  eq(count(), n + 1, "unknown bind type counts as BoE")
  assert(has('"boe":null'), last())
end)

test("loot: legendary gear always posts, legendary material never", function()
  slash("set loot min_quality 6")
  slash("set loot min_vendor_value 0")
  local n = count()
  fire("CHAT_MSG_LOOT", lootMsg(17182, "Sulfuras", 5))
  eq(count(), n + 1, "legendary weapon")
  fire("CHAT_MSG_LOOT", lootMsg(17010, "Elementium Bar", 5))
  eq(count(), n + 1, "legendary material, even with vendor 0")
  slash("reset loot")
end)

test("loot: boe and gear fields in the data", function()
  fire("CHAT_MSG_LOOT", lootMsg(13033, "Zealot Blade", 3))
  assert(has('"boe":true') and has('"gear":true'), last())
  fire("CHAT_MSG_LOOT", lootMsg(17182, "Sulfuras", 5))
  assert(has('"boe":false') and has('"gear":true'), last())
  -- Carving Knife: no GetItemInfoInstant class, so gear is unknown.
  fire("CHAT_MSG_LOOT", lootMsg(2140, "Carving Knife", 3))
  assert(has('"boe":true') and has('"gear":null'), last())
end)

test("mount: bought from a vendor via \"You receive item\"", function()
  local n = count()
  fire("MERCHANT_SHOW")
  fire("CHAT_MSG_LOOT", "You receive item: " .. link(5656, "Gray Wolf", 3))
  fire("MERCHANT_CLOSED")
  eq(count(), n + 1, "emitted")
  assert(has('"type":"mount"') and has('"name":"Gray Wolf"') and has('"item_id":5656'), last())
  assert(has('"bought":true') and has('"spell_id":null') and has('"link":"|cnIQ3:|Hitem:5656'), last())
end)

test("mount: looted mount dedupes within 120 s", function()
  now = now + 121
  local n = count()
  fire("CHAT_MSG_LOOT", lootMsg(5656, "Gray Wolf", 3))
  eq(count(), n + 1, "looted mount")
  assert(has('"bought":false'), last())
  now = now + 119
  fire("CHAT_MSG_LOOT", lootMsg(5656, "Gray Wolf", 3))
  eq(count(), n + 1, "second within 120 s dropped")
  C_MountJournal = { GetMountInfoByID = function() return "Gray Wolf", 6777 end }
  fire("NEW_MOUNT_ADDED", 12)
  eq(count(), n + 1, "journal event for the same mount dropped too")
  now = now + 121
  fire("NEW_MOUNT_ADDED", 12)
  eq(count(), n + 2, "journal route")
  assert(has('"spell_id":6777') and has('"item_id":null') and has('"link":null'), last())
  C_MountJournal = { GetMountInfoByID = function() error("boom") end }
  now = now + 121
  fire("NEW_MOUNT_ADDED", 13)
  eq(count(), n + 2, "journal error ignored")
  C_MountJournal = nil
end)

test("mount: non-mount pushed item ignored", function()
  now = now + 121
  local n = count()
  fire("CHAT_MSG_LOOT", "You receive item: " .. link(13033, "Zealot Blade", 3))
  eq(count(), n, "a weapon is not a mount")
  fire("CHAT_MSG_LOOT", "You receive item: " .. link(769, "Chunk of Boar Meat", 1) .. "x2")
  eq(count(), n, "no class info, not a mount")
end)

test("duel: win, loss ignored by default, retreat marks fled, Mak'gora flagged", function()
  local n = count()
  fire("CHAT_MSG_SYSTEM", "Flano has defeated Grakkor Highmountain in a duel")
  eq(count(), n + 1, "win")
  assert(has('"type":"duel"') and has('"opponent":"Grakkor Highmountain"') and has('"won":true'), last())
  assert(has('"fled":false') and has('"makgora":false'), last())
  fire("CHAT_MSG_SYSTEM", "Grakkor has defeated Flano Wren-Classic Beta PvE 2 in a duel")
  eq(count(), n + 1, "loss ignored by default")
  fire("CHAT_MSG_SYSTEM", "Grakkor has fled from Flano Wren in a duel")
  eq(count(), n + 2, "retreat is a win")
  assert(has('"opponent":"Grakkor"') and has('"won":true') and has('"fled":true'), last())
  fire("CHAT_MSG_SYSTEM", "Flano Wren has slain Grakkor in a Mak'gora")
  eq(count(), n + 3, "Mak'gora")
  assert(has('"makgora":true') and has('"won":true'), last())
  fire("CHAT_MSG_SYSTEM", "Bob has defeated Al in a duel")
  eq(count(), n + 3, "someone else's duel")
end)

test("duel: wins_only off records a loss", function()
  slash("set duel wins_only off")
  fire("CHAT_MSG_SYSTEM", "Grakkor has defeated Flano in a duel")
  assert(has('"opponent":"Grakkor"') and has('"won":false'), last())
  slash("reset duel")
end)

test("battleground: win recorded once per visit, loss ignored by default", function()
  local n = count()
  instanceKind, bgWinner = "pvp", nil
  fire("PLAYER_ENTERING_WORLD")
  eq(count(), n, "no winner yet")
  bgWinner = 1 -- Alliance
  fire("UPDATE_BATTLEFIELD_STATUS", 1)
  eq(count(), n + 1, "win")
  assert(has('"type":"battleground"') and has('"won":true') and has('"name":"Deadmines"'), last())
  fire("PVP_MATCH_COMPLETE")
  eq(count(), n + 1, "once per visit")
  instanceKind = "none"
  fire("PLAYER_ENTERING_WORLD")
  instanceKind, bgWinner = "pvp", 0 -- Horde wins the next one
  fire("PVP_MATCH_COMPLETE")
  eq(count(), n + 1, "loss ignored")
  instanceKind = "none"
  fire("PLAYER_ENTERING_WORLD")
  slash("set battleground wins_only off")
  instanceKind = "pvp"
  fire("PVP_MATCH_COMPLETE")
  assert(has('"won":false'), last())
  slash("reset battleground")
  instanceKind, bgWinner = "none", nil
  fire("PLAYER_ENTERING_WORLD")
end)

test("boss_kill: first kill flagged once, group lists party members, first_kill_only skips repeats", function()
  local n = count()
  partyOn = true
  fire("ENCOUNTER_END", 1145, "Mr. Smite", 1, 5, 1)
  eq(count(), n + 1, "first kill")
  assert(has('"first_kill":true'), last())
  assert(has('"group":["Grakkor Highmountain (Shaman 24)","Mira (Mage 22)"]'), last())
  eq(ns.char.kills["1145"], time(), "remembered")
  fire("ENCOUNTER_END", 1145, "Mr. Smite", 1, 5, 1)
  assert(has('"first_kill":false'), last())
  slash("set bosskill first_kill_only on")
  fire("ENCOUNTER_END", 1145, "Mr. Smite", 1, 5, 1)
  eq(count(), n + 2, "repeat skipped")
  fire("ENCOUNTER_END", 1146, "Cookie", 1, 5, 1)
  eq(count(), n + 3, "new boss still posts")
  slash("reset bosskill")
  inRaid = true
  fire("ENCOUNTER_END", 1146, "Cookie", 1, 5, 1)
  assert(has('"group":null'), "raid: " .. last())
  inRaid = false
  issecretvalue = function(v) return v == "Mira" end
  fire("ENCOUNTER_END", 1146, "Cookie", 1, 5, 1)
  assert(has('"group":null'), "secret: " .. last())
  issecretvalue = nil
  partyOn = false
  fire("ENCOUNTER_END", 1146, "Cookie", 1, 5, 1)
  assert(has('"group":null'), "solo: " .. last())
end)

test("group: off by default; on, every event lists the party; left out alone, in a raid or when secret", function()
  local function quest() fire("QUEST_TURNED_IN", 7, 100, 0) end
  partyOn = true
  quest()
  assert(not has('"group"'), "off by default: " .. last())
  slash("group on")
  eq(ns:GetOption("general", "group"), true, "/doink group on")
  quest()
  assert(has('"group":["Grakkor Highmountain (Shaman 24)","Mira (Mage 22)"]'), last())
  inRaid = true
  quest()
  assert(not has('"group"'), "raid: " .. last())
  inRaid = false
  issecretvalue = function(v) return v == "Mira" end
  quest()
  assert(not has('"group"'), "secret: " .. last())
  issecretvalue = nil
  partyOn = false
  quest()
  assert(not has('"group"'), "alone: " .. last())
  slash("group off")
  eq(ns:GetOption("general", "group"), false, "/doink group off")
end)

test("session: login and reload recorded, zoning ignored, logout recorded; never a realtime shot", function()
  local n = count()
  fire("PLAYER_ENTERING_WORLD", true, false)
  eq(count(), n + 1, "login")
  assert(has('"type":"session"') and has('"state":"login"'), last())
  fire("PLAYER_ENTERING_WORLD", false, false)
  eq(count(), n + 1, "zoning between instances isn't a session")
  fire("PLAYER_LOGOUT")
  assert(has('"state":"logout"'), last())
  fire("PLAYER_ENTERING_WORLD", false, true)
  assert(has('"state":"reload"'), last())
  eq(count(), n + 3, "three recorded")
  assert(ns.RecordOnly.session, "record-only: no screenshot, no Discord post")
end)

test("pvp_kill: our killing blows on players only, with what the client knows of the victim", function()
  GetPlayerInfoByGUID = function(guid)
    if guid == "Player-5826-0A1B2C3D" then
      return "Shaman", "SHAMAN", "Orc", "Orc", 2, "Grakkor", "", 24
    end
  end
  local me = UnitGUID("player")
  local n = count()
  fire("PARTY_KILL", me, "Player-5826-0A1B2C3D")
  eq(count(), n + 1, "a player we killed")
  assert(has('"type":"pvp_kill"') and has('"victim":"Grakkor"') and has('"class":"SHAMAN"')
    and has('"race":"Orc"') and has('"level":24') and has('"zone":"Elwynn Forest"'), last())
  fire("PARTY_KILL", me, "Creature-0-5826-0-1234-448-0000A1B2C3")
  fire("PARTY_KILL", "Player-5826-99999999", "Player-5826-0A1B2C3D")
  eq(count(), n + 1, "creatures and other people's kills ignored")
  fire("PARTY_KILL", me, "Player-5826-77777777")
  eq(count(), n + 2, "an unknown player still counts")
  assert(has('"victim":null') and has('"level":null'), last())
  issecretvalue = function(v) return v == me end
  fire("PARTY_KILL", me, "Player-5826-0A1B2C3D")
  eq(count(), n + 2, "secret GUIDs are ignored")
  issecretvalue = nil
  GetPlayerInfoByGUID = nil
end)

test("set: numbers, money and booleans, typed by the default", function()
  slash("set loot min_quality 4")
  eq(ns:GetOption("loot", "min_quality"), 4, "number")
  slash("set loot min_vendor_value 1g50s")
  eq(ns:GetOption("loot", "min_vendor_value"), 15000, "money")
  slash("set SkillUp milestones_only off")
  eq(ns:GetOption("skill_up", "milestones_only"), false, "boolean")
  local out = slash("options loot")
  assert(out:find("min_vendor_value = 1g 50s", 1, true) and out:find("(custom)", 1, true), out)
end)

test("set: rejects unknown options and bad values without changing anything", function()
  assert(slash("set loot colour red"):find("no option 'colour'", 1, true))
  assert(slash("set loot min_quality lots"):find("bad value", 1, true))
  assert(slash("set loot min_vendor_value 1g50"):find("bad value", 1, true))
  eq(ns:GetOption("loot", "min_quality"), 4, "unchanged")
end)

test("set: option thresholds actually filter", function()
  local n = count()
  fire("CHAT_MSG_LOOT", "You receive loot: " .. link(2140, "Carving Knife", 3))
  eq(count(), n, "rare below min_quality 4")
end)

test("reset: back to defaults", function()
  slash("reset loot")
  slash("reset skillup")
  eq(ns:GetOption("loot", "min_quality"), 3, "default quality")
  eq(ns:GetOption("skill_up", "milestones_only"), true, "default milestones")
  assert(not slash("options loot"):find("(custom)", 1, true))
  assert(not slash("options skillup"):find("(custom)", 1, true))
end)

test("webhook: account-wide, per character, clear; never printed in full", function()
  local url = "https://discord.com/api/webhooks/123456/AbC_d-ef9"
  slash("webhook " .. url)
  eq(DOINKDB.webhooks["*"], url, "account")
  slash("webhook here " .. url .. "X")
  eq(DOINKDB.webhooks[NEW_KEY], url .. "X", "char")
  local status = slash("")
  assert(status:find("this character, ending ...ef9X", 1, true), status)
  assert(not status:find("123456", 1, true), "status leaks the URL")
  slash("webhook here clear")
  eq(DOINKDB.webhooks[NEW_KEY], nil, "char cleared")
  assert(slash(""):find("all characters", 1, true))
  assert(slash("webhook https://evil.example/api/webhooks/1/x"):find("doesn't look like", 1, true))
  eq(DOINKDB.webhooks["*"], url, "bad URL ignored")
  slash("webhook clear")
  eq(DOINKDB.webhooks["*"], nil, "account cleared")
end)

test("every notifier has a working /doink test fixture", function()
  for _, t in ipairs({ "levelup", "loot", "death", "quest", "bosskill", "skillup",
      "mount", "duel", "battleground", "session", "pvpkill" }) do
    local n = count()
    SlashCmdList.DOINK("test " .. t)
    eq(count(), n + 1, t)
    assert(has('"test":true'), last())
  end
end)

------------------------------------------------------------------ realtime

local Shot = ns.Transports.Screenshot
local T = Shot._test

local function bitString(row, n)
  local out = {}
  for c = 1, n do out[c] = tostring(row[c] or 0) end
  return table.concat(out)
end

local function field(row, offset, width) -- MSB-first integer from row bits
  local v = 0
  for i = offset + 1, offset + width do v = v * 2 + (row[i] or 0) end
  return v
end

-- Runs the transport until its queue is empty, answering every shot.
local function drainShots()
  for _ = 1, 200 do
    if #T.queue() == 0 and T.state() == "idle" then return end
    now = now + 0.5
    T.update()
    if T.state() == "shooting" then fire("SCREENSHOT_SUCCEEDED") end
  end
  error("queue never drained")
end

test("realtime: off by default and creates nothing until enabled", function()
  eq(DOINKDB.realtime.enabled, false, "default")
  eq(DOINKDB.realtime.image, false, "image default, added to an older realtime table")
  slash("test levelup")
  eq(T.grid(), nil, "no grid while off")
  eq(T.driver(), nil, "no driver while off")
  eq(screenshots, 0, "no screenshots")
  assert(slash(""):find("realtime: off", 1, true))
end)

test("realtime: CRCs match the standard check values", function()
  eq(T.crc16("123456789"), 0x29B1, "CRC-16/CCITT-FALSE")
  eq(T.crc8("123456789"), 0xF4, "CRC-8")
end)

test("realtime: grid layout, header fields and padding", function()
  local msg = string.rep("x", 150)
  local rows = Shot.Encode(msg, 0, 256)
  eq(#rows, 1 + 5, "header row + ceil(1200/256) data rows")
  local row0 = rows[1]
  eq(bitString(row0, 16), "1010101010110011", "sync")
  eq(field(row0, 16, 4), 2, "version")
  eq(field(row0, 20, 4), 0, "flags")
  eq(field(row0, 24, 16), 256, "cols")
  eq(field(row0, 40, 8), 5, "rows")
  eq(field(row0, 48, 16), 150, "payload length")
  eq(field(row0, 64, 16), T.crc16(msg), "payload crc")
  local header = ""
  for i = 0, 7 do header = header .. string.char(field(row0, 16 + i * 8, 8)) end
  eq(field(row0, 80, 8), T.crc8(header), "header crc")
  eq(bitString(row0, 256):sub(89), string.rep("0", 256 - 88), "row 0 padding")
  eq(bitString(rows[2], 16), "0111100001111000", "data bits ('x' = 0x78)")
  -- 1200 bits: 4 full rows, then 176 bits, then padding.
  eq(bitString(rows[6], 256):sub(177), string.rep("0", 80), "last row padding")
  for r = 1, 6 do eq(#rows[r], 256, "row " .. r .. " width") end
  eq(field(Shot.Encode("hi", 1, 256)[1], 20, 4), 1, "hello flag")
  eq(field(Shot.Encode("hi", 2, 256)[1], 20, 4), 2, "image flag")
  eq(field(Shot.Encode("hi", 3, 256)[1], 20, 4), 3, "hello and image flags")
  eq(#Shot.Encode(msg, 0, 1200), 1 + 1, "a wide strip: one data row")
end)

test("realtime: one screenshot holds up to 1024 bytes", function()
  eq(#Shot.Encode(string.rep("a", 1024), 0, 256), 33, "32 data rows at the narrowest")
  eq(Shot.Encode(string.rep("a", 1025), 0, 1200), nil, "too long")
end)

test("realtime: an event shows the grid for one frame: shoot, then hide", function()
  slash("realtime on")
  local g = T.grid()
  assert(g and g.parent == UIParent, "grid parented to UIParent")
  eq(g.strata, "TOOLTIP", "strata"); eq(g.mouse, false, "mouse off")
  eq(g.ignoreParentScale, true, "ignores parent scale")
  assert(math.abs(g.scale - 768 / 1080) < 1e-9, "pixel-perfect scale")
  eq(g.point[1], "TOPLEFT", "corner"); eq(g.point[4], 0, "x"); eq(g.point[5], 0, "y")
  eq(T.block(), 2, "2 px blocks")
  eq(T.cols(), 959, "as wide as 1920 px allows, one block kept for the backing")
  eq(T.queue()[1], nil, "no hello at enable")

  now = now + 10
  local before = screenshots
  slash("test levelup")
  eq(#T.queue(), 1, "queued")
  T.update()
  -- The client captures the frame drawn after the call: show and shoot
  -- in the same frame, hide on the next.
  assert(g:IsShown(), "grid shown")
  eq(screenshots, before + 1, "shot in the same frame")
  eq(T.state(), "shooting", "waiting for the client")
  -- Painted colours follow the bits: the sync starts 1, 0.
  local tex = T.textures()
  eq(tex[1][1].color, 1, "sync starts white"); eq(tex[1][2].color, 0, "then black")
  T.update()
  assert(not g:IsShown(), "hidden on the next frame")
  eq(#T.queue(), 1, "still waiting for the client")
  T.update()
  eq(screenshots, before + 1, "one shot")
  fire("SCREENSHOT_SUCCEEDED")
  eq(#T.queue(), 0, "done")
  assert(slash("realtime"):find("last shot ok", 1, true), "status")
end)

test("realtime: shots are 1.1 s apart, the queue is capped at 8", function()
  now = now + 10
  slash("test levelup")
  slash("test levelup")
  local shotTime = now
  T.update(); T.update(); T.update(); fire("SCREENSHOT_SUCCEEDED")
  eq(#T.queue(), 1, "one left")
  T.update()
  eq(T.state(), "idle", "waits for the spacing")
  now = shotTime + 1.2
  T.update()
  eq(T.state(), "shooting", "then shoots the next")
  drainShots()

  slash("test levelup 10")
  eq(#T.queue(), 8, "capped")
  drainShots()
end)

test("realtime: a failed shot is retried once, a silent one times out", function()
  now = now + 10
  local before = screenshots
  slash("test death")
  T.update(); T.update(); T.update()
  fire("SCREENSHOT_FAILED")
  eq(#T.queue(), 1, "kept for one retry")
  now = now + 2
  T.update(); T.update(); T.update()
  fire("SCREENSHOT_FAILED")
  eq(#T.queue(), 0, "dropped after two tries")
  eq(screenshots, before + 2, "two shots")

  now = now + 2
  slash("test death")
  T.update(); T.update(); T.update()
  now = now + 3.5
  T.update()
  eq(#T.queue(), 0, "timed out and moved on")
  eq(T.state(), "idle", "idle")
  assert(not T.grid():IsShown(), "hidden")
end)

test("realtime: waits while the UI is hidden (Alt-Z)", function()
  now = now + 10
  UIParent:Hide()
  slash("test levelup")
  T.update(); T.update(); T.update()
  eq(T.state(), "idle", "nothing painted with the UI hidden")
  UIParent:Show()
  T.update()
  eq(T.state(), "shooting", "shot once it's back")
  drainShots()
end)

test("realtime: unreadable screenshot settings are swapped for the shot and put back", function()
  now = now + 10
  cvars.screenshotFormat, cvars.screenshotQuality = "tga", "1"
  slash("test levelup")
  T.update(); T.update(); T.update()
  eq(cvars.screenshotFormat, "jpeg", "jpeg during the shot")
  eq(cvars.screenshotQuality, "3", "quality 3 during the shot")
  eq(DOINKDB.realtime.restore.format, "tga", "old format remembered")
  fire("SCREENSHOT_SUCCEEDED")
  eq(cvars.screenshotFormat, "tga", "format restored")
  eq(cvars.screenshotQuality, "1", "quality restored")
  eq(DOINKDB.realtime.restore, nil, "nothing left to restore")

  -- A /reload mid-shot leaves the swap in place; login puts it back.
  cvars.screenshotFormat = "jpeg"
  DOINKDB.realtime.restore = { format = "tga" }
  Shot.OnLogin()
  eq(cvars.screenshotFormat, "tga", "restored at login")
  eq(DOINKDB.realtime.restore, nil, "cleared")
  cvars.screenshotFormat, cvars.screenshotQuality = "jpeg", "3"
end)

test("realtime: image on sets flag bit 1 on events, never on hellos", function()
  now = now + 10
  slash("realtime image on")
  eq(DOINKDB.realtime.image, true, "on")
  assert(slash("realtime"):find("image on", 1, true), "status")
  slash("test levelup")
  eq(field(T.queue()[1].rows[1], 20, 4), 2, "event carries the image flag")
  drainShots()
  now = now + 10
  slash("realtime test")
  eq(field(T.queue()[1].rows[1], 20, 4), 1, "hello has only the hello flag")
  drainShots()
  now = now + 10
  slash("realtime image off")
  eq(DOINKDB.realtime.image, false, "off")
  assert(slash("realtime"):find("image off", 1, true), "status")
  slash("test levelup")
  eq(field(T.queue()[1].rows[1], 20, 4), 0, "no flag when off")
  drainShots()
  assert(slash("realtime image"):find("realtime image: off", 1, true), "prints the state")
  eq(DOINKDB.realtime.image, false, "asking changes nothing")
end)

test("realtime: session events are never shot (they wait for /reload or logout)", function()
  local wasOn = DOINKDB.realtime.enabled
  slash("realtime on")
  drainShots()
  local n = count()
  fire("PLAYER_ENTERING_WORLD", true, false)
  eq(count(), n + 1, "recorded")
  eq(#T.queue(), 0, "no screenshot queued for a login")
  if not wasOn then slash("realtime off") end
end)

test("realtime: a hello for /doink realtime test", function()
  now = now + 10
  assert(slash("realtime test"):find("Realtime works", 1, true))
  local rows = T.queue()[1].rows
  eq(field(rows[1], 20, 4), 1, "hello flag")
  drainShots()
end)

test("defaults: everything is off until the player enables it", function()
  for t, v in pairs(SHIPPED) do eq(v, false, t .. " enabled by default") end
  eq(SHIPPED_ANNOUNCE.channel, "off", "announce channel")
  for _, k in ipairs({ "death", "quest", "boss_kill", "mount", "duel", "battleground" }) do
    eq(SHIPPED_ANNOUNCE[k], false, "announce " .. k)
  end
  eq(SHIPPED_ANNOUNCE.level_up, "off", "announce level_up")
  eq(SHIPPED_ANNOUNCE.loot, "off", "announce loot")
end)

test("realtime: blocked by the client turns it off", function()
  slash("test levelup")
  fire("ADDON_ACTION_BLOCKED", "SomeOtherAddon", "Foo()")
  eq(DOINKDB.realtime.enabled, true, "another addon's block is ignored")
  fire("ADDON_ACTION_BLOCKED", "DOINK", "SendChatMessage()")
  eq(DOINKDB.realtime.enabled, true, "another DOINK call being blocked is ignored")
  printed = {}
  fire("ADDON_ACTION_BLOCKED", "DOINK", "Screenshot()")
  eq(DOINKDB.realtime.enabled, false, "off")
  eq(#T.queue(), 0, "queue cleared")
  assert(table.concat(printed, "\n"):find("blocks addon screenshots", 1, true), "told")
  assert(slash("realtime"):find("blocked", 1, true), "status says why")
  local before = screenshots
  slash("test levelup")
  T.update(); T.update(); T.update()
  eq(screenshots, before, "nothing shot while off")
end)

test("realtime: off clears the queue; old pixel settings are gone", function()
  slash("realtime on")
  slash("test levelup 3")
  slash("realtime off")
  eq(#T.queue(), 0, "cleared")
  eq(DOINKDB.realtime.position, nil, "position migrated away")
  eq(DOINKDB.realtime.block, nil, "block migrated away")
  assert(slash("realtime position topleft"):find("no longer needed", 1, true))
end)

test("realtime: fixture matches (shared with the companion's decoder tests)", function()
  local payload = '{"char":"Flano","class":"WARRIOR","data":{"level":10},"level":10,'
    .. '"realm":"Classic Beta PvE 2","seq":3,"surname":"Wren","test":true,'
    .. '"ts":1790870000,"type":"level_up"}'
  local rows = Shot.Encode(payload, 0, 400)
  local lines = { "payload " .. payload }
  for r = 1, #rows do lines[#lines + 1] = bitString(rows[r], 400) end
  local text = table.concat(lines, "\n") .. "\n"
  local path = FIXTURES .. "/shot_levelup.txt"
  if os.getenv("DOINK_WRITE_FIXTURE") then
    local f = assert(io.open(path, "w")); f:write(text); f:close()
  end
  local f = assert(io.open(path, "r"), "fixture missing; run with DOINK_WRITE_FIXTURE=1")
  local expected = f:read("*a"); f:close()
  eq(text, expected, "fixture")
end)

------------------------------------------------------------------ announce

local Announce = ns.Transports.Announce

local function announceReset()
  runTimers()
  Announce._test.reset()
  chat = {}
  timers = {}
end

local function lastChat() return chat[#chat] end

test("announce: level milestones go to guild chat by default, other levels don't", function()
  announceReset()
  inGuild = true
  fire("PLAYER_LEVEL_UP", 19)
  eq(#chat, 0, "19 not announced")
  fire("PLAYER_LEVEL_UP", 20)
  eq(#chat, 1, "20 announced")
  eq(lastChat()[1], "Ding! Level 20.", "wording"); eq(lastChat()[2], "GUILD", "channel")
  runTimers()
  fire("PLAYER_LEVEL_UP", 60)
  eq(lastChat()[1], "Ding! Level 60 - max level!", "max level wording")
end)

test("announce: loot needs epic by default; rule rare lets blues through, with the link", function()
  announceReset()
  slash("reset loot") -- make sure the queue's own rare threshold is default
  fire("CHAT_MSG_LOOT", "You receive loot: " .. link(2140, "Carving Knife", 3))
  eq(#chat, 0, "rare not announced by default")
  assert(slash("announce loot rare"):find("announce loot: rare", 1, true))
  fire("CHAT_MSG_LOOT", "You receive loot: " .. link(2140, "Carving Knife", 3))
  eq(lastChat()[1], "Looted " .. link(2140, "Carving Knife", 3) .. "!", "link kept intact")
  runTimers()
  slash("set loot min_quality 0")
  fire("CHAT_MSG_LOOT", "You receive loot: " .. link(4875, "Broken Boar Tusk", 0) .. "x2")
  eq(#chat, 1, "poor quality stack not announced even though queued")
  slash("reset loot")
  slash("announce loot epic")
end)

test("announce: deaths and boss kills yes, quests off unless asked", function()
  announceReset()
  die()
  eq(lastChat()[1], "Died in Elwynn Forest.", "death")
  runTimers()
  local recap = { { sourceName = "Hogger" } }
  DeathRecap_HasEvents = function() return true end
  DeathRecap_GetEvents = function() return recap end
  die(); runTimers()
  eq(lastChat()[1], "Killed by Hogger in Elwynn Forest.", "killer named")
  recap = { { environmentalType = "Falling" } }
  die(); runTimers()
  eq(lastChat()[1], "Forgot I couldn't fly. Died in Elwynn Forest.", "environmental")
  DeathRecap_HasEvents, DeathRecap_GetEvents = nil, nil
  fire("QUEST_TURNED_IN", 33, 450, 75)
  eq(#chat, 3, "quest not announced by default")
  slash("announce quest on")
  fire("QUEST_COMPLETE")
  fire("QUEST_TURNED_IN", 33, 450, 75)
  eq(lastChat()[1], "Completed quest: Wolves Across the Border.", "quest wording")
  slash("announce quest off")
  runTimers()
  fire("ENCOUNTER_END", 1144, "Edwin VanCleef", 1, 5, 1)
  eq(lastChat()[1], "Edwin VanCleef down! (Deadmines, 5 players)", "boss wording")
end)

test("announce: nothing without a guild; party and raid routing", function()
  announceReset()
  inGuild = false
  fire("PLAYER_LEVEL_UP", 30)
  eq(#chat, 0, "no guild, no line")
  assert(slash(""):find("guild (not in a guild)", 1, true))
  slash("announce party")
  fire("PLAYER_LEVEL_UP", 30)
  eq(#chat, 0, "not grouped")
  inGroup = true
  fire("PLAYER_LEVEL_UP", 30)
  eq(lastChat()[2], "PARTY", "party chat")
  runTimers()
  slash("announce raid")
  inRaid = true
  fire("PLAYER_LEVEL_UP", 40)
  eq(lastChat()[2], "RAID", "raid chat")
  inGroup, inRaid = false, false
  slash("announce guild")
  inGuild = true
end)

test("announce: test events are whispered to you, never the guild", function()
  announceReset()
  slash("test levelup")
  eq(lastChat()[2], "WHISPER", "whisper"); eq(lastChat()[3], "Flano Wren", "full name")
  assert(lastChat()[1]:find("^%[test%] Ding! Level"), lastChat()[1])
  runTimers()
  now = now + 61 -- fresh per-minute budget: nine sample lines follow
  slash("announce test")
  runTimers()
  local lines = {}
  for _, c in ipairs(chat) do lines[#lines + 1] = c[1] end
  local all = table.concat(lines, "\n")
  assert(all:find("Thunderfury", 1, true) and all:find("would announce)", 1, true), all)
  assert(all:find("Completed quest: A Threat Within. (would not announce)", 1, true), all)
  for _, c in ipairs(chat) do eq(c[2], "WHISPER", "all whispers") end
end)

test("announce: one line every 2s, at most 8 a minute, dropped lines are reported", function()
  announceReset()
  for lvl = 10, 50, 10 do fire("PLAYER_LEVEL_UP", lvl) end
  eq(#chat, 1, "first line sent at once, the rest wait")
  eq(#timers, 1, "a 2s timer is pending"); eq(timers[1][1], 2, "gap")
  runTimers()
  eq(#chat, 5, "all five out after the timers")
  printed = {}
  for lvl = 10, 50, 10 do fire("PLAYER_LEVEL_UP", lvl) end
  runTimers()
  eq(#chat, 8, "capped at 8 in the same minute")
  assert(table.concat(printed, "\n"):find("announcement dropped", 1, true), "player told")
  now = now + 61
  fire("PLAYER_LEVEL_UP", 60)
  runTimers()
  eq(#chat, 9, "new minute, new budget")
end)

test("announce: off silences everything, including tests; bad input shows usage", function()
  announceReset()
  slash("announce off")
  fire("PLAYER_LEVEL_UP", 20)
  slash("test levelup")
  runTimers()
  eq(#chat, 0, "silent")
  assert(slash("announce loot shiny"):find("usage", 1, true))
  assert(slash("announce bogus"):find("usage", 1, true))
  slash("announce guild")
  assert(slash(""):find("announce: guild; level_up milestones, loot epic, death on, quest off, boss_kill on, skill_up off, mount on, duel on, battleground on", 1, true))
end)

test("announce: mounts, duels, battlegrounds and First kill!", function()
  announceReset()
  now = now + 121
  fire("MERCHANT_SHOW")
  fire("CHAT_MSG_LOOT", "You receive item: " .. link(5656, "Gray Wolf", 3))
  fire("MERCHANT_CLOSED")
  eq(lastChat()[1], "Bought a new mount: " .. link(5656, "Gray Wolf", 3) .. "!", "bought mount")
  runTimers()
  now = now + 121
  C_MountJournal = { GetMountInfoByID = function() return "Swift Razzashi Raptor", 24242 end }
  fire("NEW_MOUNT_ADDED", 1)
  C_MountJournal = nil
  eq(lastChat()[1], "Got a new mount: Swift Razzashi Raptor!", "journal mount, no link")
  runTimers()
  fire("CHAT_MSG_SYSTEM", "Grakkor has fled from Flano in a duel")
  eq(lastChat()[1], "Won a duel against Grakkor (they ran).", "duel win, fled")
  runTimers()
  fire("CHAT_MSG_SYSTEM", "Flano has slain Grakkor in a Mak'gora")
  eq(lastChat()[1], "Won a Mak'gora against Grakkor.", "Mak'gora")
  runTimers()
  slash("set duel wins_only off")
  fire("CHAT_MSG_SYSTEM", "Grakkor has defeated Flano in a duel")
  eq(lastChat()[1], "Lost a duel to Grakkor.", "duel loss")
  slash("reset duel")
  runTimers()
  now = now + 61
  instanceKind, bgWinner = "pvp", 1
  fire("PVP_MATCH_COMPLETE")
  eq(lastChat()[1], "Won Deadmines!", "battleground win")
  runTimers()
  slash("set battleground wins_only off")
  instanceKind = "none"; fire("PLAYER_ENTERING_WORLD")
  instanceKind, bgWinner = "pvp", 0
  fire("PVP_MATCH_COMPLETE")
  eq(lastChat()[1], "Lost Deadmines.", "battleground loss")
  slash("reset battleground")
  instanceKind, bgWinner = "none", nil; fire("PLAYER_ENTERING_WORLD")
  runTimers()
  fire("ENCOUNTER_END", 1147, "Captain Greenskin", 1, 5, 1)
  eq(lastChat()[1], "Captain Greenskin down! (Deadmines, 5 players) First kill!", "first kill")
  runTimers()
  fire("ENCOUNTER_END", 1147, "Captain Greenskin", 1, 5, 1)
  eq(lastChat()[1], "Captain Greenskin down! (Deadmines, 5 players)", "repeat kill")
  runTimers()
end)

------------------------------------------------------------------ chat on its own

-- The suite runs with every type enabled (see SHIPPED above), which is not
-- what a new player has: these tests switch the Discord toggles off again.
local ALL_TYPES = { "level_up", "loot", "death", "quest", "boss_kill", "skill_up",
  "mount", "duel", "battleground", "session", "pvp_kill" }

-- Sets `enabled` for the given types; returns a function that puts back
-- whatever overrides were there.
local function setEnabled(types, value)
  local old = {}
  for _, t in ipairs(types) do
    old[t] = ns.char.config[t] and ns.char.config[t].enabled
    ns:SetOption(t, "enabled", value)
  end
  return function()
    for _, t in ipairs(types) do ns.char.config[t].enabled = old[t] end
  end
end

test("announce: works with every Discord toggle off; nothing is queued or shot", function()
  announceReset()
  slash("announce level_up all")
  slash("realtime on")
  -- A transport that didn't ask for unqueued events must never see one.
  local stubCalls = 0
  ns.Transports.Stub = { Send = function() stubCalls = stubCalls + 1 end }
  fire("PLAYER_LEVEL_UP", 18)
  eq(stubCalls, 1, "a queued event reaches every transport")
  runTimers(); drainShots(); chat = {}
  local restore = setEnabled(ALL_TYPES, false)
  local n, seq = count(), ns.char.seq
  fire("PLAYER_LEVEL_UP", 19)
  ns.Transports.Stub = nil
  eq(stubCalls, 1, "an unqueued event only goes to transports that ask for it")
  eq(#chat, 1, "announced")
  eq(lastChat()[1], "Ding! Level 19.", "wording"); eq(lastChat()[2], "GUILD", "channel")
  eq(count(), n, "nothing queued for the companion")
  eq(ns.char.seq, seq, "no seq used")
  eq(#T.queue(), 0, "no screenshot for an event that isn't recorded")
  restore()
  slash("realtime off")
  slash("announce level_up milestones")
end)

test("announce: the Discord filters don't limit chat", function()
  announceReset()
  local n = count()

  slash("set levelup milestones_only on")
  slash("announce level_up all")
  fire("PLAYER_LEVEL_UP", 21); runTimers()
  eq(lastChat()[1], "Ding! Level 21.", "level_up milestones_only vs announce all")
  eq(count(), n, "level 21 not queued")
  slash("set levelup milestones_only off") -- not reset: a later test reads this type's overrides
  slash("announce level_up milestones")

  slash("announce skill_up all") -- skill_up milestones_only is on by default
  fire("CHAT_MSG_SKILL", "Your skill in Blacksmithing has increased to 149."); runTimers()
  eq(lastChat()[1], "Blacksmithing 149/225.", "skill_up milestones_only vs announce all")
  eq(count(), n, "rank 149 not queued")
  slash("announce skill_up off")

  slash("reset loot") -- min_quality 3
  slash("announce loot uncommon")
  fire("CHAT_MSG_LOOT", lootMsg(2079, "Sergeant's Blade", 2)); runTimers()
  eq(lastChat()[1], "Looted " .. link(2079, "Sergeant's Blade", 2) .. "!", "a green is announced")
  eq(count(), n, "the green is not queued")
  slash("announce loot epic")

  slash("set bosskill first_kill_only on")
  fire("ENCOUNTER_END", 1150, "Gilnid", 1, 5, 1); runTimers()
  eq(count(), n + 1, "first kill queued")
  eq(lastChat()[1], "Gilnid down! (Deadmines, 5 players) First kill!", "first kill announced")
  fire("ENCOUNTER_END", 1150, "Gilnid", 1, 5, 1); runTimers()
  eq(lastChat()[1], "Gilnid down! (Deadmines, 5 players)", "second kill announced")
  eq(count(), n + 1, "second kill not queued")
  slash("reset bosskill")

  eq(#chat, 5, "one line each")
end)

test("announce: wins_only ignores a loss for chat too; off, chat decides on its own", function()
  announceReset()
  local n = count()
  local function bgLoss()
    instanceKind, bgWinner = "pvp", 0 -- Horde wins; we're Alliance
    fire("PVP_MATCH_COMPLETE"); runTimers()
    instanceKind, bgWinner = "none", nil
    fire("PLAYER_ENTERING_WORLD")
  end

  -- wins_only on (the default): nothing announced, nothing queued.
  fire("CHAT_MSG_SYSTEM", "Grakkor has defeated Flano in a duel"); runTimers()
  bgLoss()
  eq(#chat, 0, "losses not announced"); eq(count(), n, "losses not queued")

  -- wins_only off, Discord toggle off: announced, not queued.
  local restore = setEnabled({ "duel", "battleground" }, false)
  ns:SetOption("duel", "wins_only", false)
  ns:SetOption("battleground", "wins_only", false)
  fire("CHAT_MSG_SYSTEM", "Grakkor has defeated Flano in a duel"); runTimers()
  eq(lastChat()[1], "Lost a duel to Grakkor.", "duel loss announced")
  bgLoss()
  eq(lastChat()[1], "Lost Deadmines.", "battleground loss announced")
  eq(#chat, 2, "one line each"); eq(count(), n, "not queued while disabled")
  ns.char.config.duel.wins_only, ns.char.config.battleground.wins_only = nil, nil
  restore()
end)

test("announce: an enabled event that passes its filter is queued as before and announced once", function()
  announceReset()
  local n = count()
  fire("PLAYER_LEVEL_UP", 30)
  runTimers()
  eq(count(), n + 1, "queued once")
  eq(last(), '{"char":"Flano","class":"WARRIOR","data":{"level":30},"level":30,'
    .. '"realm":"Classic Beta PvE 2","seq":' .. ns.char.seq .. ',"surname":"Wren",'
    .. '"ts":1790870000,"type":"level_up"}', "JSON unchanged")
  eq(#chat, 1, "announced once")
  eq(lastChat()[1], "Ding! Level 30.", "wording")
end)

test("test: a disabled type still whispers its line and says it wasn't recorded", function()
  announceReset()
  local restore = setEnabled({ "level_up" }, false)
  local n, seq = count(), ns.char.seq
  local out = slash("test levelup")
  eq(#chat, 1, "whispered")
  eq(lastChat()[2], "WHISPER", "to you only")
  assert(lastChat()[1]:find("^%[test%] Ding! Level"), lastChat()[1])
  eq(count(), n, "not queued"); eq(ns.char.seq, seq, "no seq used")
  assert(out:find("not recorded for Discord", 1, true) and out:find("disabled", 1, true), out)
  restore()
  assert(slash("test levelup"):find("emitted 1 test level_up", 1, true), "enabled again: recorded")
end)

test("announce: a send that errors doesn't stall the lines after it", function()
  announceReset()
  local real, calls = SendChatMessage, 0
  SendChatMessage = function(...)
    calls = calls + 1
    if calls == 1 then error("boom") end
    return real(...)
  end
  pcall(fire, "PLAYER_LEVEL_UP", 20) -- this send throws
  fire("PLAYER_LEVEL_UP", 30)
  runTimers()
  SendChatMessage = real
  eq(#chat, 1, "the next line went out after the timer")
  eq(lastChat()[1], "Ding! Level 30.", "which line")
end)

test("announce: a chat send blocked by the game is reported once and counted", function()
  announceReset()
  printed = {}
  fire("ADDON_ACTION_BLOCKED", "DOINK", "SendChatMessage()")
  fire("ADDON_ACTION_FORBIDDEN", "DOINK", "SendChatMessage()")
  fire("ADDON_ACTION_BLOCKED", "SomeOtherAddon", "SendChatMessage()")
  fire("ADDON_ACTION_BLOCKED", "DOINK", "Foo()")
  local notices = 0
  for _, line in ipairs(printed) do
    if line:find("blocked a chat announcement", 1, true) then notices = notices + 1 end
  end
  eq(notices, 1, "one notice for two blocked sends")
  assert(slash(""):find("2 blocked by the game", 1, true), "status shows the count")
  announceReset()
  assert(not slash(""):find("blocked by the game", 1, true), "no count when nothing was blocked")
end)

------------------------------------------------------------------ options page

local function control(variable)
  for _, c in ipairs(settings.controls) do
    if c.variable == "DOINK_" .. variable then return c end
  end
end

test("options: registered once at login with every control and three headers", function()
  eq(settings.categories, 1, "one category")
  assert(settings.registered, "added to the AddOns list")
  eq(#settings.controls, 7 + 3 + 13 + 7 + 1 + 2, "announce 7+3, discord 13+7+1, realtime 2")
  local kinds = { checkbox = 0, dropdown = 0, slider = 0 }
  for _, c in ipairs(settings.controls) do kinds[c.kind] = kinds[c.kind] + 1 end
  eq(kinds.checkbox, 27, "checkboxes"); eq(kinds.dropdown, 5, "dropdowns"); eq(kinds.slider, 1, "slider")
  for _, c in ipairs(settings.controls) do assert(c.name and c.name ~= "", c.variable .. " has a label") end
  ns.Options.Register()
  eq(settings.categories, 1, "registering again is a no-op")
end)

test("options: controls read and write the same options as the slash commands", function()
  local channel = control("announce_channel")
  eq(channel.get(), "guild", "reads current")
  eq(#channel.options, 5, "five channels"); eq(channel.options[1].label, "Guild chat", "label")
  channel.set("party")
  eq(ns:GetOption("announce", "channel"), "party", "writes through")
  assert(slash("announce"):find("announce: party", 1, true), "slash sees it")
  slash("announce guild")
  eq(channel.get(), "guild", "slash change visible to the page")

  local vendor = control("loot_min_vendor_value")
  eq(vendor.get(), 1, "1g default shown in gold")
  vendor.set(5)
  eq(ns:GetOption("loot", "min_vendor_value"), 50000, "stored in copper")
  slash("reset loot")

  local quality = control("loot_min_quality")
  eq(quality.options[4].label, "Rare (blue)", "quality labels")
  quality.set(4); eq(ns:GetOption("loot", "min_quality"), 4, "number dropdown")
  slash("reset loot")

  local enabled = control("enabled_death")
  enabled.set(false); eq(ns:GetOption("death", "enabled"), false, "checkbox")
  enabled.set(true)
end)

test("options: the realtime checkbox turns the screenshot transport on and off", function()
  local rt = control("realtime_enabled")
  eq(rt.get(), false, "off by default")
  rt.set(true)
  eq(DOINKDB.realtime.enabled, true, "enabled")
  assert(T.driver(), "transport started")
  rt.set(false)
  eq(DOINKDB.realtime.enabled, false, "disabled")
end)

test("options: the image checkbox follows /doink realtime image", function()
  local img = control("realtime_image")
  eq(img.get(), false, "off by default")
  img.set(true)
  eq(DOINKDB.realtime.image, true, "on")
  img.set(false)
  eq(DOINKDB.realtime.image, false, "off")
  slash("realtime image on")
  eq(img.get(), true, "slash command shows in the checkbox")
  slash("realtime image off")
end)

test("options: /doink config opens the page", function()
  slash("config")
  eq(settings.opened, 42, "opened by category id")
end)

test("upgrade: v0.17 characters keep their effective settings, new ones start off", function()
  local key = ns.player.key
  -- The migrated character from the top of the file.
  local c = ns.db.chars[key].config
  eq(c.level_up.enabled, true, "old default pinned")
  eq(c.quest.enabled, true, "explicit value kept")
  eq(c.announce.channel, "guild", "announce channel pinned")
  eq(ns.db.chars[key].optIn, true, "marked")

  -- A character saved by v0.17: one override, the rest implicit defaults.
  ns.db.chars[key] = { seq = 5, events = {}, kills = {},
    config = { death = { enabled = false }, announce = { channel = "party" } } }
  fire("PLAYER_LOGIN")
  c = ns.db.chars[key].config
  eq(c.death.enabled, false, "a notifier the player turned off stays off")
  eq(c.announce.channel, "party", "a chosen channel is not overwritten")
  eq(c.loot.enabled, true, "implicit default pinned")
  eq(c.announce.loot, "epic", "implicit announce rule pinned")
  eq(ns.db.chars[key].seq, 5, "queue untouched")

  -- Logging in again changes nothing, even if the player turns things off.
  ns.db.chars[key].config.loot.enabled = false
  fire("PLAYER_LOGIN")
  eq(ns.db.chars[key].config.loot.enabled, false, "pinned once only")

  -- A brand-new character: nothing stored, shipped defaults (all off).
  ns.db.chars[key] = nil
  fire("PLAYER_LOGIN")
  eq(next(ns.db.chars[key].config), nil, "no overrides for a new character")
  eq(ns.db.chars[key].optIn, true, "marked")
end)

------------------------------------------------------------------ time played

-- Runs last: once the total is known it stays known for the session.
test("played: off by default, nothing asked and no field", function()
  eq(ns.Defaults.general.played, false, "shipped off")
  eq(playedRequests, 0, "never asked while off, not even at the logins above")
  fire("PLAYER_LEVEL_UP", 31)
  assert(not has('"played"'), last())
  assert(slash(""):find("time played in events: off", 1, true), "status line")
end)

test("played: a client without the API still records events, without the field", function()
  local real = RequestTimePlayed
  RequestTimePlayed = nil
  slash("played on")
  local n = count()
  fire("PLAYER_LEVEL_UP", 31)
  RequestTimePlayed = real
  assert(count() > n, "recorded")
  assert(not has('"played"'), last())
  eq(_G["ChatFrame1"]:IsEventRegistered("TIME_PLAYED_MSG"), true, "chat frames untouched")
  slash("played off")
end)

test("played: a client that never answers gets its chat lines back", function()
  timers = {}
  slash("played on")
  eq(playedRequests, 1, "asked")
  eq(_G["ChatFrame1"]:IsEventRegistered("TIME_PLAYED_MSG"), false, "hidden while waiting")
  eq(#timers, 1, "one fallback timer"); eq(timers[1][1], 5, "after 5 s")
  runTimers()
  eq(_G["ChatFrame1"]:IsEventRegistered("TIME_PLAYED_MSG"), true, "listening again")
  eq(_G["ChatFrame2"]:IsEventRegistered("TIME_PLAYED_MSG"), true, "all of them")
  slash("played off")
  playedRequests = 0
  now = now + 11 -- past the gap between two requests
end)

test("played: turning it on asks once, quietly, and events carry the running total", function()
  timers = {}
  local out = slash("played on")
  assert(out:find("time played on", 1, true), out)
  eq(playedRequests, 1, "asked")
  eq(_G["ChatFrame1"]:IsEventRegistered("TIME_PLAYED_MSG"), false, "chat frames stopped listening")

  -- Not answered yet: the event goes out without the field, and isn't asked again.
  fire("PLAYER_LEVEL_UP", 32)
  assert(not has('"played"'), last())
  eq(playedRequests, 1, "one request, not one per event")

  fire("TIME_PLAYED_MSG", 408475, 2462)
  eq(playedLines, 0, "the game's two chat lines stayed hidden")
  -- Only the restore the answer scheduled, not the 5 s fallback.
  for i = #timers, 1, -1 do
    if timers[i][1] == 5 then table.remove(timers, i) end
  end
  eq(#timers, 1, "the answer schedules the restore")
  runTimers()
  eq(_G["ChatFrame1"]:IsEventRegistered("TIME_PLAYED_MSG"), true, "chat frames listen again")
  eq(_G["ChatFrame2"]:IsEventRegistered("TIME_PLAYED_MSG"), true, "all of them")

  now = now + 90.6
  fire("PLAYER_LEVEL_UP", 33)
  eq(last(), '{"char":"Flano","class":"WARRIOR","data":{"level":33},"level":33,"played":408565,'
    .. '"realm":"Classic Beta PvE 2","seq":' .. ns.char.seq .. ',"surname":"Wren",'
    .. '"ts":1790870000,"type":"level_up"}', "total + seconds since, whole seconds")
  eq(playedRequests, 1, "counted on, not asked again")
  assert(slash(""):find("time played in events: on", 1, true), "status line")

  -- The player's own /played refreshes the total and prints as usual.
  fire("TIME_PLAYED_MSG", 500000, 10)
  eq(playedLines, 2, "their /played shows in chat")
  runTimers()
  fire("QUEST_TURNED_IN", 818, 625, 0)
  assert(has('"played":500000'), last())
end)

test("played: off again drops the field; bad arguments only explain", function()
  assert(slash("played off"):find("time played off", 1, true))
  fire("PLAYER_LEVEL_UP", 34)
  assert(not has('"played"'), last())
  local out = slash("played maybe")
  assert(out:find("usage: /doink played on|off", 1, true), out)
  eq(ns:GetOption("general", "played"), false, "unchanged")
end)

test("played: the settings checkbox is the same switch", function()
  local c = control("general_played")
  eq(c.get(), false, "off")
  c.set(true)
  eq(ns:GetOption("general", "played"), true, "on")
  fire("PLAYER_LEVEL_UP", 35)
  assert(has('"played":'), last())
  c.set(false)
end)

print = io.write
io.write(string.format("%d passed, %d failed\n", passed, failed))
os.exit(failed == 0 and 0 or 1)
