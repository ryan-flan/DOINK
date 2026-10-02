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
  function f:GetEffectiveScale() return 1 end
  function f:CreateTexture()
    local t = {}
    function t:SetSize(w, h) self.w, self.h = w, h end
    function t:SetPoint(_, _, _, x, y) self.x, self.y = x, y end
    function t:SetColorTexture(r) self.color = r end
    f.textures[#f.textures + 1] = t
    return t
  end
  frames[#frames + 1] = f
  return f
end
WorldFrame = CreateFrame("Frame")
UIParent = CreateFrame("Frame")
function GetPhysicalScreenSize() return 1920, 1080 end

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
function UnitName() return "Paul", "Hebbs" end -- Forever: surname second
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
    "Notifiers/Quest.lua", "Notifiers/BossKill.lua", "Notifiers/SkillUp.lua",
    "Transports/Pixel.lua" }) do
  assert(loadfile(ROOT .. "/" .. file))("DOINK", ns)
end

-- SavedVariables as written by v0.2.0, before surnames were known.
local OLD_KEY, NEW_KEY = "Paul-Classic Beta PvE 2", "Paul Hebbs-Classic Beta PvE 2"
local OLD_HOOK = "https://discord.com/api/webhooks/9/old"
DOINKDB = {
  version = 1,
  webhooks = { [OLD_KEY] = OLD_HOOK },
  chars = {
    [OLD_KEY] = {
      seq = 2,
      config = { quest = { enabled = true } },
      events = {
        '{"char":"Paul","realm":"Classic Beta PvE 2","seq":1,"type":"level_up"}',
        '{"char":"Paul","realm":"Classic Beta PvE 2","seq":2,"type":"quest"}',
      },
    },
  },
}
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

test("surname: pre-surname entry migrates to the full-name key", function()
  eq(DOINKDB.chars[OLD_KEY], nil, "old key gone")
  local char = DOINKDB.chars[NEW_KEY]
  assert(char, "new key exists")
  eq(char.seq, 2, "seq carried over")
  eq(char.config.quest.enabled, true, "settings carried over")
  eq(DOINKDB.webhooks[NEW_KEY], OLD_HOOK, "per-char webhook carried over")
  eq(DOINKDB.webhooks[OLD_KEY], nil, "old webhook key gone")
  -- Queued events gain the surname and stay otherwise intact.
  eq(char.events[1], '{"surname":"Hebbs","char":"Paul","realm":"Classic Beta PvE 2","seq":1,"type":"level_up"}', "event 1")
  assert(char.events[2]:find('^{"surname":"Hebbs","char":"Paul"'), char.events[2])
  DOINKDB.webhooks[NEW_KEY] = nil -- keep the webhook tests below independent
end)

test("surname: new events carry it, seq continues", function()
  SlashCmdList.DOINK("test levelup")
  assert(has('"char":"Paul"') and has('"surname":"Hebbs"') and has('"seq":3,'), last())
end)

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
  for _, t in ipairs({ "levelup", "loot", "death", "quest", "bosskill", "skillup" }) do
    local n = count()
    SlashCmdList.DOINK("test " .. t)
    eq(count(), n + 1, t)
    assert(has('"test":true'), last())
  end
end)

------------------------------------------------------------------ realtime

local Pixel = ns.Transports.Pixel
local T = Pixel._test

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

test("realtime: off by default and creates nothing until enabled", function()
  eq(DOINKDB.realtime.enabled, false, "default")
  slash("test levelup")
  eq(T.frame(), nil, "no frame while off")
  assert(slash(""):find("realtime (experimental): off", 1, true))
end)

test("realtime: CRCs match the standard check values", function()
  eq(T.crc16("123456789"), 0x29B1, "CRC-16/CCITT-FALSE")
  eq(T.crc8("123456789"), 0xF4, "CRC-8")
end)

test("realtime: chunk layout, header fields and padding", function()
  local msg = string.rep("x", 150)
  local chunks = Pixel.Encode(400, 0x1234, msg)
  eq(#chunks, 2, "150 bytes at 100/chunk")
  local row0 = chunks[2][1]
  eq(bitString(row0, 16), "1010101010110011", "sync")
  eq(field(row0, 16, 4), 1, "version")
  eq(field(row0, 20, 4), 0, "flags")
  eq(field(row0, 24, 16), 400, "blocks")
  eq(field(row0, 40, 16), 0x1234, "msg id")
  eq(field(row0, 56, 8), 1, "chunk index")
  eq(field(row0, 64, 8), 2, "chunk count")
  eq(field(row0, 72, 8), 50, "payload length of the last chunk")
  eq(field(row0, 80, 16), T.crc16(msg:sub(101)), "payload crc")
  eq(#bitString(row0, 400), 400, "row 0 padded to width")
  -- First data row of chunk 1: 'x' = 0x78 = 01111000, repeated.
  eq(bitString(chunks[1][2], 16), "0111100001111000", "data bits")
  -- Chunk 2 has 50 bytes = 400 bits: fills row 1 exactly, row 2 is padding.
  eq(bitString(chunks[2][3], 400), string.rep("0", 400), "padding")
end)

test("realtime: on builds the strip, says hello, cycles, then hides", function()
  slash("realtime on")
  local f = T.frame()
  -- UIParent, not WorldFrame: WorldFrame children draw under the whole UI.
  assert(f and f.parent == UIParent, "frame parented to UIParent")
  eq(f.strata, "TOOLTIP", "strata"); eq(f.mouse, false, "mouse off")
  eq(f.ignoreParentScale, true, "ignores parent scale")
  assert(math.abs(f.scale - 768 / 1080) < 1e-9, "pixel-perfect scale")
  local n, b = T.geometry()
  eq(n, 400, "blocks (1920/3 capped at 400)"); eq(b, 3, "block px")
  eq(#f.textures, 3 * 400, "one texture per block")
  eq(f.w, 1200, "width px"); eq(f.h, 9, "height px")
  eq(f.point[1], "TOPLEFT", "default corner")

  assert(f:IsShown(), "hello shows the strip immediately")
  local outbox = T.outbox()
  eq(#outbox, 1, "hello queued")
  local chunks = #outbox[1].chunks
  assert(chunks >= 1, "hello encoded")
  local first = T.current()
  assert(first == outbox[1].chunks[1], "chunk 1 painted first")
  -- Painted colours follow the bits.
  eq(f.textures[1].color, 1, "sync starts white"); eq(f.textures[2].color, 0, "then black")

  T.advance(0.1)
  assert(T.current() == first, "nothing changes before 0.25s")
  T.advance(0.15)
  if chunks > 1 then assert(T.current() == outbox[1].chunks[2], "chunk 2 next") end
  for _ = 2, chunks * 4 do T.advance(0.25) end
  assert(not f:IsShown(), "hidden after 4 showings")
  eq(#T.outbox(), 0, "outbox drained")
end)

test("realtime: emitted events are queued, outbox is capped at 8", function()
  slash("test levelup 10")
  eq(#T.outbox(), 8, "capped")
  assert(T.frame():IsShown(), "showing")
  slash("realtime off")
  eq(#T.outbox(), 0, "cleared on off")
  assert(not T.frame():IsShown(), "hidden on off")
  slash("test levelup")
  eq(#T.outbox(), 0, "nothing queued while off")
end)

test("realtime: position and block commands", function()
  slash("realtime on")
  assert(slash("realtime position top-right"):find("topright", 1, true))
  eq(T.frame().point[1], "TOPRIGHT", "repositioned")
  eq(DOINKDB.realtime.position, "topright", "saved")
  assert(slash("realtime position middle"):find("usage", 1, true))
  assert(slash("realtime block 9"):find("usage", 1, true))
  slash("realtime block 4")
  eq(DOINKDB.realtime.block, 4, "block saved for next reload")
  assert(slash("realtime"):find("on, topright", 1, true))
  slash("realtime off")
end)

test("realtime: fixture matches (shared with the companion's decoder tests)", function()
  local payload = '{"char":"Paul","class":"WARRIOR","data":{"level":10},"level":10,'
    .. '"realm":"Classic Beta PvE 2","seq":3,"surname":"Hebbs","test":true,'
    .. '"ts":1790870000,"type":"level_up"}'
  local chunks = Pixel.Encode(400, 4660, payload)
  local lines = { "msg_id 4660", "blocks 400", "rows 3", "payload " .. payload }
  for i, rows in ipairs(chunks) do
    lines[#lines + 1] = "chunk " .. (i - 1)
    for r = 1, 3 do lines[#lines + 1] = bitString(rows[r], 400) end
  end
  local text = table.concat(lines, "\n") .. "\n"
  local path = FIXTURES .. "/pixel_levelup.txt"
  if os.getenv("DOINK_WRITE_FIXTURE") then
    local f = assert(io.open(path, "w")); f:write(text); f:close()
  end
  local f = assert(io.open(path, "r"), "fixture missing; run with DOINK_WRITE_FIXTURE=1")
  local expected = f:read("*a"); f:close()
  eq(text, expected, "fixture")
end)

print = io.write
io.write(string.format("%d passed, %d failed\n", passed, failed))
os.exit(failed == 0 and 0 or 1)
