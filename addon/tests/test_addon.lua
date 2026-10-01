-- Offline test harness: stubs just enough of the WoW API to load the addon
-- and drive it with real-format events. Lua 5.1 / LuaJIT.
--   cd addon && luajit tests/test_addon.lua

local ROOT = (arg and arg[0] or ""):match("^(.*)/tests/") or "."
ROOT = ROOT .. "/DOINK"

------------------------------------------------------------------ WoW stubs

local frames = {}
function CreateFrame()
  local f = { events = {} }
  function f:RegisterEvent(e) self.events[e] = true end
  function f:UnregisterEvent(e) self.events[e] = nil end
  function f:SetScript(_, fn) self.onEvent = fn end
  frames[#frames + 1] = f
  return f
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

local now, level = 1000, 9
time = function() return 1790870000 end
function GetTime() return now end
function UnitName() return "Paul" end
function UnitGUID() return "Player-1-ABC" end
function GetRealmName() return "Classic Beta PvE 2" end
function UnitClass() return "Warrior", "WARRIOR" end
function UnitLevel() return level end
function GetZoneText() return "Elwynn Forest" end
function GetSubZoneText() return "" end
function GetInstanceInfo() return "Deadmines", "party" end
function GetTitleText() return "Wolves Across the Border" end
function GetAddOnMetadata() return "test" end
function ReloadUI() end
function geterrorhandler() return function(e) error(e, 0) end end
SlashCmdList = {}

-- name, link, quality, ilvl, reqLevel, class, subclass, maxStack, equipLoc, texture, sellPrice
local itemCache = {
  [769] = { "Chunk of Boar Meat", nil, 1, 0, 0, nil, nil, 20, "", 0, 4 },
  [2140] = { "Carving Knife", nil, 3, 0, 0, nil, nil, 1, "", 0, 260 },
  [4875] = { "Broken Boar Tusk", nil, 0, 0, 0, nil, nil, 1, "", 0, 20000 },
}
local uncached = {}
function GetItemInfo(id)
  if uncached[id] then return nil end
  local row = itemCache[id]
  if row then return unpack(row, 1, 11) end
end

PROTECTED_EVENTS = { COMBAT_LOG_EVENT_UNFILTERED = true }

local skills = {
  { "Professions", true }, { "Blacksmithing", false, nil, 150, 0, 0, 225 },
}
function GetNumSkillLines() return #skills end
function GetSkillLineInfo(i) return unpack(skills[i], 1, 7) end

local printed = {}
print = function(s) printed[#printed + 1] = s end

------------------------------------------------------------------ load

local ns = {}
for _, file in ipairs({ "Json.lua", "Defaults.lua", "Core.lua",
    "Notifiers/LevelUp.lua", "Notifiers/Loot.lua", "Notifiers/Death.lua",
    "Notifiers/Quest.lua", "Notifiers/BossKill.lua", "Notifiers/SkillUp.lua" }) do
  assert(loadfile(ROOT .. "/" .. file))("DOINK", ns)
end
fire("ADDON_LOADED", "DOINK")
fire("PLAYER_LOGIN")

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

test("FormatToPattern escapes magic and captures", function()
  local p = ns.FormatToPattern("You receive loot: %sx%d.")
  local l, q = ("You receive loot: [a.b]x3."):match(p)
  eq(l, "[a.b]", "link"); eq(q, "3", "qty")
  eq(("You receive loot: [a]x3!"):match(p), nil, "trailing char must not match")
end)

test("loot: real beta message, below threshold, is ignored", function()
  local n = count()
  fire("CHAT_MSG_LOOT", "You receive loot: " .. link(769, "Chunk of Boar Meat", 1), "Paul")
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

test("no notifier registers events protected in Forever", function()
  -- Registering these pops "DOINK has been blocked from an action only
  -- available to the Blizzard UI" at load. See CLAUDE.md beta facts.
  for _, f in ipairs(frames) do
    for event in pairs(PROTECTED_EVENTS) do
      assert(not f.events[event], event .. " is registered")
    end
  end
end)

test("death: zone and subzone, killer unknown", function()
  fire("PLAYER_DEAD")
  assert(has('"type":"death"') and has('"zone":"Elwynn Forest"'), last())
  assert(has('"subzone":null') and has('"killer":null'), last())
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

test("every notifier has a working /doink test fixture", function()
  for _, t in ipairs({ "levelup", "loot", "death", "quest", "bosskill", "skillup" }) do
    local n = count()
    SlashCmdList.DOINK("test " .. t)
    eq(count(), n + 1, t)
    assert(has('"test":true'), last())
  end
end)

print = io.write
io.write(string.format("%d passed, %d failed\n", passed, failed))
os.exit(failed == 0 and 0 or 1)
