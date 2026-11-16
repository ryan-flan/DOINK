local ADDON, ns = ...

-- Data contract constants (see CLAUDE.md before changing).
local DB_VERSION = 1
local MAX_EVENTS = 500

-- Notifier files (loaded after this one) register themselves here.
ns.Notifiers = {}
-- Transports do something with each event the moment it happens: announce
-- it in chat, show it to the companion. Core never knows what; it hands
-- them the JSON and the envelope. Each exposes Send(json, envelope),
-- OnLogin() and Status(). A transport with `unqueued = true` also gets
-- events that aren't recorded for the companion (see ns:Emit); for those
-- json is nil and the envelope has no seq.
ns.Transports = {}

local frame = CreateFrame("Frame")
local handlers = {} -- event name -> list of notifiers listening to it

------------------------------------------------------------------ output

local PREFIX = "|cff66ccffDOINK|r: "

function ns:Print(msg, ...)
  if select("#", ...) > 0 then msg = msg:format(...) end
  print(PREFIX .. msg)
end

function ns:Debug(msg, ...)
  if DOINKDB and DOINKDB.debug then
    self:Print("|cff999999" .. msg .. "|r", ...)
  end
end

-- Forever has Midnight's "secret values": during a boss encounter the
-- client hands addons some event arguments (chat text, unit names) it
-- won't let them read. tostring() of one yields another secret, and
-- table.concat throws "invalid value (secret)" (user report, 2026-10-03,
-- entering an encounter). issecretvalue() tells them apart.
function ns.IsSecret(value)
  return issecretvalue ~= nil and issecretvalue(value) or false
end

local function ArgsToString(...)
  local parts = {}
  for i = 1, select("#", ...) do
    local value = (select(i, ...))
    if ns.IsSecret(value) then
      parts[i] = "<secret>"
    else
      local ok, text = pcall(tostring, value)
      parts[i] = ok and text or "?"
    end
  end
  return table.concat(parts, ", ")
end

------------------------------------------------------------------ helpers

-- Turns a GlobalStrings format such as LOOT_ITEM_SELF ("You receive loot: %s")
-- into an anchored Lua pattern: %s -> (.+), %d -> (%d+). Matching against
-- the client's own strings survives rewording (Forever dropped vanilla's
-- trailing periods) and other locales.
function ns.FormatToPattern(fmt)
  local p = fmt:gsub("%%s", "\001"):gsub("%%d", "\002")
  p = p:gsub("[%^%$%(%)%%%.%[%]%*%+%-%?]", "%%%0")
  p = p:gsub("\001", "(.+)"):gsub("\002", "(%%d+)")
  return "^" .. p .. "$"
end

-- "%1$s has defeated %2$s in a duel" -> anchored pattern + the argument
-- number each capture holds (so swapped-order locales still work). Plain
-- %s/%d count in order.
function ns.FormatToPatternOrdered(fmt)
  local parts, order, nextArg, i = {}, {}, 1, 1
  while i <= #fmt do
    local n, kind = fmt:match("^%%(%d+)%$([sd])", i)
    local plain = not n and fmt:match("^%%([sd])", i)
    if n then
      parts[#parts + 1] = kind == "d" and "(%d+)" or "(.-)"
      order[#order + 1] = tonumber(n)
      i = i + #n + 3
    elseif plain then
      parts[#parts + 1] = plain == "d" and "(%d+)" or "(.+)"
      order[#order + 1] = nextArg
      nextArg = nextArg + 1
      i = i + 2
    else
      parts[#parts + 1] = (fmt:sub(i, i):gsub("[%^%$%(%)%%%.%[%]%*%+%-%?]", "%%%0"))
      i = i + 1
    end
  end
  return "^" .. table.concat(parts) .. "$", order
end

-- Captures from an ordered pattern, rearranged into argument order:
-- args[1] is what %1$s held, whatever its position in the text.
function ns.MatchOrdered(text, pattern, order)
  local captures = { text:match(pattern) }
  if #captures == 0 then return nil end
  local args = {}
  for i, argIndex in ipairs(order) do args[argIndex] = captures[i] end
  return args
end

------------------------------------------------------------------ options

function ns:GetOption(eventType, key)
  local overrides = self.char and self.char.config[eventType]
  if overrides and overrides[key] ~= nil then
    return overrides[key]
  end
  local defaults = ns.Defaults[eventType]
  return defaults and defaults[key]
end

function ns:SetOption(eventType, key, value)
  local config = self.char.config
  config[eventType] = config[eventType] or {}
  config[eventType][key] = value
end

------------------------------------------------------------------ database

-- ADDON_LOADED: SavedVariables are available from here on.
local function InitDB()
  if type(DOINKDB) ~= "table" then DOINKDB = {} end
  local db = DOINKDB
  db.version = db.version or DB_VERSION
  db.chars = db.chars or {}
  db.webhooks = db.webhooks or {} -- ["*"] = account-wide, ["Name-Realm"] = per char
  -- Realtime (screenshot transport) is opt-in; see Transports/Screenshot.lua.
  db.realtime = db.realtime or { enabled = false }
  -- v0.17.0: let the companion attach each screenshot to the Discord post.
  if db.realtime.image == nil then db.realtime.image = false end
  -- v0.14.0: the pixel strip's corner and block size are gone.
  db.realtime.position, db.realtime.block = nil, nil
  -- Future migrations go here:
  -- if db.version < 2 then ... db.version = 2 end
  ns.db = db
end

-- Before surnames, chars were keyed "Flano-Realm". Move that entry (queue,
-- settings, per-char webhook) to "Flano Wren-Realm" and stamp the surname
-- into its queued events, so the companion sees one character, not two.
local function MigrateToSurname(name, surname, realm, key)
  local oldKey = name .. "-" .. realm
  local old = ns.db.chars[oldKey]
  if not old or ns.db.chars[key] then return end

  local stamp = '{"surname":' .. ns.Json.Encode(surname) .. ","
  for i, json in ipairs(old.events or {}) do
    if not json:find('"surname":', 1, true) then
      old.events[i] = json:gsub("^{", function() return stamp end, 1)
    end
  end
  ns.db.chars[key], ns.db.chars[oldKey] = old, nil
  ns.db.webhooks[key] = ns.db.webhooks[key] or ns.db.webhooks[oldKey]
  ns.db.webhooks[oldKey] = nil
  ns:Print("moved saved data from %s to %s", oldKey, key)
end

-- v0.18.0 made everything off by default. A character that already existed
-- was running on the old defaults without ever storing them, so pin those
-- values as explicit overrides once; nothing the player sees changes. Only
-- keys the player hasn't set are filled. New characters skip this.
local LEGACY_DEFAULTS = {
  level_up = { enabled = true }, loot = { enabled = true }, death = { enabled = true },
  quest = { enabled = true }, boss_kill = { enabled = true }, skill_up = { enabled = true },
  mount = { enabled = true }, duel = { enabled = true }, battleground = { enabled = true },
  announce = { channel = "guild", level_up = "milestones", loot = "epic", death = true,
    boss_kill = true, mount = true, duel = true, battleground = true },
}

local function PinLegacyDefaults(char)
  for eventType, options in pairs(LEGACY_DEFAULTS) do
    local overrides = char.config[eventType]
    if overrides == nil then
      overrides = {}
      char.config[eventType] = overrides
    end
    for key, value in pairs(options) do
      if overrides[key] == nil then overrides[key] = value end
    end
  end
end

-- PLAYER_LOGIN: name, realm and class are reliable from here on.
local function InitChar()
  -- Forever: UnitName("player") returns the surname second (verified in
  -- beta). Other clients return nil there for the player.
  local name, surname = UnitName("player")
  if surname == "" then surname = nil end
  local fullName = surname and (name .. " " .. surname) or name
  local realm = GetRealmName()
  local key = fullName .. "-" .. realm -- Forever only guarantees full names are unique

  if surname then MigrateToSurname(name, surname, realm, key) end

  local isNew = ns.db.chars[key] == nil
  local char = ns.db.chars[key] or {}
  char.seq = char.seq or 0
  char.config = char.config or {}
  if not char.optIn then
    if not isNew then PinLegacyDefaults(char) end
    char.optIn = true -- v0.18.0 defaults handled for this character
  end
  char.events = char.events or {}
  char.kills = char.kills or {} -- encounter id (string) -> ts of the first kill
  ns.db.chars[key] = char

  ns.char = char
  ns.player = {
    key = key,
    name = name,
    surname = surname, -- nil outside Forever
    realm = realm,
    class = select(2, UnitClass("player")), -- "WARRIOR", not localized
  }
end

------------------------------------------------------------------ time played

-- Optional `played` field on every event (general.played, off by default).
-- The client only tells the total on request: RequestTimePlayed() is
-- answered with TIME_PLAYED_MSG(total, thisLevel), both in seconds
-- (verified on Forever 2026-11-15, called from a timer). We ask once and
-- count on from there with GetTime().
local played       -- { total = seconds, at = GetTime() }, once known
local playedAsked  -- GetTime() of the last request
local playedHidden -- chat frames that get TIME_PLAYED_MSG back

local function RestorePlayedChat()
  for _, chatFrame in ipairs(playedHidden or {}) do
    chatFrame:RegisterEvent("TIME_PLAYED_MSG")
  end
  playedHidden = nil
end

local function RequestPlayed()
  if not RequestTimePlayed then return end
  if playedAsked and GetTime() - playedAsked < 10 then return end
  if not pcall(frame.RegisterEvent, frame, "TIME_PLAYED_MSG") then return end
  playedAsked = GetTime()
  -- The chat frames print "Total time played: ..." for this event. Take
  -- it away from them until our answer is in, so asking stays quiet. If
  -- this client's chat frames don't work that way, the two lines show.
  playedHidden = playedHidden or {}
  C_Timer.After(5, RestorePlayedChat) -- no answer: /played must keep working
  for i = 1, NUM_CHAT_WINDOWS or 0 do
    local chatFrame = _G["ChatFrame" .. i]
    if chatFrame and chatFrame.IsEventRegistered and chatFrame:IsEventRegistered("TIME_PLAYED_MSG") then
      chatFrame:UnregisterEvent("TIME_PLAYED_MSG")
      playedHidden[#playedHidden + 1] = chatFrame
    end
  end
  RequestTimePlayed()
end

local function OnTimePlayed(total)
  if type(total) == "number" and not ns.IsSecret(total) then
    played = { total = total, at = GetTime() }
  end
  -- Not now: a frame registered while the event is being handed out
  -- might still get this one.
  C_Timer.After(0, RestorePlayedChat)
end

-- Total seconds played, or nil when the option is off or the client
-- hasn't answered yet (asking is started then, for the next event).
function ns:Played()
  if not self:GetOption("general", "played") then return nil end
  if not played then
    RequestPlayed()
    return nil
  end
  return math.floor(played.total + (GetTime() - played.at))
end

function ns:SetPlayed(on)
  self:SetOption("general", "played", on)
  self:Played() -- on: ask now, so the next event has it
end

------------------------------------------------------------------ group

-- "WARRIOR" -> "Warrior"
local function TitleCase(word)
  return word:sub(1, 1):upper() .. word:sub(2):lower()
end

-- Party members other than you, as "Name Surname (Class Level)", from
-- party1-party4. nil when you're alone, in a raid, or when the client hides
-- any of it (secret values). Under the 12.0 secret rules a party or raid
-- member's name and class aren't secret (SecretWhenUnitIdentityRestricted:
-- "when the unit isn't player-controlled or in the party/raid", Forever
-- 1.60.1 API docs), but every value is still checked: one secret value
-- would take table.concat and the JSON encoder down.
--
-- Raids are left out on purpose: 39 names don't fit a realtime screenshot
-- (1024 bytes per shot).
function ns:PartyList()
  if IsInRaid() then return nil end
  local list = {}
  for i = 1, 4 do
    local unit = "party" .. i
    if UnitExists(unit) then
      local name, second = UnitName(unit)
      local _, classFile = UnitClass(unit)
      local level = UnitLevel(unit)
      if ns.IsSecret(name) or ns.IsSecret(second) or ns.IsSecret(classFile)
          or ns.IsSecret(level) then
        return nil
      end
      if type(name) == "string" then
        local display = name .. (second and second ~= "" and (" " .. second) or "")
        local class = type(classFile) == "string" and TitleCase(classFile) or "?"
        list[#list + 1] = ("%s (%s %d)"):format(display, class, tonumber(level) or 0)
      end
    end
  end
  if #list == 0 then return nil end
  return list
end

-- The optional `group` field (general.group, off by default): who else was
-- in your party when the event happened. Left out when the option is off,
-- when you're alone or in a raid, or when the client hides a name.
function ns:Group()
  if not self:GetOption("general", "group") then return nil end
  return self:PartyList()
end

------------------------------------------------------------------ emit

-- Types that are only recorded for tools reading forward_url: no realtime
-- screenshot, no chat, and the companion doesn't post them to Discord.
ns.RecordOnly = { session = true }

-- Builds the envelope and hands the event to the transports.
--
-- An event is queued (seq assigned, JSON stored for the companion) when its
-- type is enabled and the notifier's own filter let it through. `filtered`
-- is the notifier saying "my filter rejects this one". An event that isn't
-- queued gets no seq, is stored nowhere, and is only offered to transports
-- that ask for unqueued events: chat announcements follow their own rules
-- and need neither the Discord toggle nor its filters.
--
-- Returns the envelope and whether it was queued; nil before login.
function ns:Emit(eventType, data, isTest, filtered)
  if not self.char then
    self:Print("dropped %s: emitted before PLAYER_LOGIN", eventType)
    return
  end
  local enabled = self:GetOption(eventType, "enabled")
  local queued = enabled and not filtered

  local char = self.char
  if queued then
    char.seq = char.seq + 1 -- never reused, even if encoding below fails
  end

  -- UnitLevel lags behind during PLAYER_LEVEL_UP (verified in beta), so
  -- also trust the level that event delivered.
  local level = math.max(UnitLevel("player"), self.knownLevel or 0)

  local envelope = {
    seq = queued and char.seq or nil, -- nil leaves the field out
    ts = time(),
    char = self.player.name,
    surname = self.player.surname, -- nil leaves the field out
    realm = self.player.realm,
    class = self.player.class,
    level = level,
    played = self:Played(), -- nil leaves the field out
    group = self:Group(), -- nil leaves the field out
    type = eventType,
    data = data or {},
  }
  if isTest then envelope.test = true end

  local json
  if queued then
    json = ns.Json.Encode(envelope)
    local events = char.events
    events[#events + 1] = json
    while #events > MAX_EVENTS do
      table.remove(events, 1)
    end
    self:Debug("emit %s", json)
  else
    self:Debug("%s not recorded (%s)", eventType, enabled and "filtered" or "disabled")
  end

  for _, transport in pairs(ns.Transports) do
    if queued or transport.unqueued then
      -- A transport bug must never cost the queued event above.
      xpcall(function() transport.Send(json, envelope) end, geterrorhandler())
    end
  end
  return envelope, queued
end

------------------------------------------------------------------ dispatch

local function RegisterNotifiers()
  for _, notifier in pairs(ns.Notifiers) do
    for _, event in ipairs(notifier.events) do
      if not handlers[event] then
        -- Unknown events throw. Warn instead, so one event missing from
        -- Forever doesn't take the whole addon down.
        if pcall(frame.RegisterEvent, frame, event) then
          handlers[event] = {}
        else
          ns:Print("|cffff6060unknown event %s (%s disabled)|r", event, notifier.type)
        end
      end
      if handlers[event] then
        table.insert(handlers[event], notifier)
      end
    end
  end
end

-- Events too frequent to echo in /doink debug.
local QUIET_EVENTS = {
  GET_ITEM_INFO_RECEIVED = true, -- fires for every tooltip, not just loot
}

local function Dispatch(event, ...)
  local list = handlers[event]
  if not list then return end

  -- Only format the arguments when someone will see them: this used to run
  -- for every event with debug off, and a secret argument took the whole
  -- dispatch down before any notifier ran.
  if DOINKDB.debug and not QUIET_EVENTS[event] then
    local ok, text = pcall(ArgsToString, ...)
    ns:Debug("%s: %s", event, ok and text or "(unprintable arguments)")
  end
  local n, args = select("#", ...), { ... }
  for _, notifier in ipairs(list) do
    -- geterrorhandler() reports to BugSack but lets other notifiers run.
    xpcall(function()
      notifier.OnEvent(event, unpack(args, 1, n))
    end, geterrorhandler())
  end
end

frame:SetScript("OnEvent", function(self, event, ...)
  if event == "ADDON_LOADED" then
    if ... == ADDON then
      InitDB()
      self:UnregisterEvent("ADDON_LOADED")
    end
  elseif event == "PLAYER_LOGIN" then
    InitChar()
    RegisterNotifiers()
    for _, transport in pairs(ns.Transports) do
      xpcall(transport.OnLogin, geterrorhandler())
    end
    if ns.Options then
      xpcall(ns.Options.Register, geterrorhandler())
    end
    ns:Played() -- if the option is on: ask at login, before the first event
  elseif event == "TIME_PLAYED_MSG" then
    OnTimePlayed(...)
  else
    if event == "PLAYER_LEVEL_UP" then
      ns.knownLevel = ... -- before notifiers run, so their Emit sees it
    end
    Dispatch(event, ...)
  end
end)

frame:RegisterEvent("ADDON_LOADED")
frame:RegisterEvent("PLAYER_LOGIN")
frame:RegisterEvent("PLAYER_LEVEL_UP") -- for ns.knownLevel, even if LevelUp is disabled

------------------------------------------------------------------ slash commands

local function SortedNotifiers()
  local list = {}
  for _, notifier in pairs(ns.Notifiers) do list[#list + 1] = notifier end
  table.sort(list, function(a, b) return a.type < b.type end)
  return list
end

local function TypeList()
  local types = {}
  for i, notifier in ipairs(SortedNotifiers()) do types[i] = notifier.type end
  return table.concat(types, ", ")
end

-- Matches "levelup", "level_up", "LevelUp".
local function FindNotifier(query)
  query = query:lower():gsub("_", "")
  for _, notifier in pairs(ns.Notifiers) do
    if (notifier.type:gsub("_", "")) == query then return notifier end
  end
end

local function FindOrComplain(query)
  local notifier = FindNotifier(query)
  if not notifier then
    ns:Print("unknown type '%s'. Types: %s", query, TypeList())
  end
  return notifier
end

local HELP = {
  "/doink - status",
  "/doink config - open the settings page (also under Options > AddOns)",
  "/doink enable|disable <type> - record <type> for Discord (chat announcements don't need it)",
  "/doink options [type] - show settings",
  "/doink set <type> <option> <value> - e.g. set loot min_quality 4",
  "/doink reset <type> - back to defaults",
  "/doink announce [guild|officer|party|raid|off] - where to announce in chat",
  "/doink announce <type> <rule> - e.g. announce loot rare, announce quest on (types: level_up, loot, death, quest, boss_kill, skill_up, mount, duel, battleground)",
  "/doink announce test - whisper yourself a sample of every announcement",
  "/doink webhook [here] <url>|clear - Discord webhook (here = this character only)",
  "/doink played on|off - add your total time played to every recorded event",
  "/doink group on|off - add who's in your party to every recorded event",
  "/doink realtime on|off|test|image on|off - post to Discord instantly via screenshots (needs the companion)",
  "/doink test <type> [count] - emit a fake event",
  "/doink dump [n] - print the last n queued events",
  "/doink debug - toggle verbose logging",
  "/doink flush - /reload to write SavedVariables",
}

local function FormatMoney(copper)
  local g, s, c = math.floor(copper / 10000), math.floor(copper / 100) % 100, copper % 100
  local parts = {}
  if g > 0 then parts[#parts + 1] = g .. "g" end
  if s > 0 then parts[#parts + 1] = s .. "s" end
  if c > 0 or #parts == 0 then parts[#parts + 1] = c .. "c" end
  return table.concat(parts, " ")
end

-- "1g50s", "75s", "30c" -> copper. nil unless the whole string is money.
local function ParseMoney(text)
  text = text:lower():gsub("%s", "")
  if text == "" or text:gsub("%d+[gsc]", "") ~= "" then return nil end
  local copper = 0
  for amount, unit in text:gmatch("(%d+)([gsc])") do
    copper = copper + tonumber(amount) * (unit == "g" and 10000 or unit == "s" and 100 or 1)
  end
  return copper
end

-- Options take their type from the default, so Defaults.lua is the schema;
-- string options must be one of ns.Choices.
local function ParseValue(default, text, choices)
  if type(default) == "boolean" then
    text = text:lower()
    if text == "on" or text == "true" or text == "yes" or text == "1" then return true end
    if text == "off" or text == "false" or text == "no" or text == "0" then return false end
  elseif type(default) == "number" then
    return tonumber(text) or ParseMoney(text)
  elseif type(default) == "string" then
    text = text:lower()
    for _, choice in ipairs(choices or {}) do
      if choice == text then return choice end
    end
  end
end

local function FormatValue(key, value)
  if type(value) == "boolean" then return value and "on" or "off" end
  if key:find("value$") then return FormatMoney(value) end -- copper amounts
  return tostring(value)
end

local function SortedKeys(t)
  local keys = {}
  for k in pairs(t) do keys[#keys + 1] = k end
  table.sort(keys)
  return keys
end

local WEBHOOK_PATTERNS = {
  "^https://discord%.com/api/webhooks/%d+/[%w_%-]+$",
  "^https://discordapp%.com/api/webhooks/%d+/[%w_%-]+$",
}

local function IsWebhook(url)
  for _, pattern in ipairs(WEBHOOK_PATTERNS) do
    if url:match(pattern) then return true end
  end
  return false
end

-- Never print a whole webhook URL: anyone with it can post to the channel.
local function WebhookStatus()
  local hooks = DOINKDB.webhooks
  local url, scope = hooks[ns.player.key], "this character"
  if not url then url, scope = hooks["*"], "all characters" end
  if not url then return "not set (companion falls back to config.toml)" end
  return ("%s, ending ...%s"):format(scope, url:sub(-4))
end

local commands = {}

commands.help = function()
  for _, line in ipairs(HELP) do ns:Print(line) end
end

commands[""] = function()
  local GetMeta = C_AddOns and C_AddOns.GetAddOnMetadata or GetAddOnMetadata
  local char = ns.char
  ns:Print("v%s - %s", GetMeta(ADDON, "Version") or "?", ns.player.key)
  ns:Print("queue %d/%d, last seq %d, debug %s",
    #char.events, MAX_EVENTS, char.seq, DOINKDB.debug and "on" or "off")
  ns:Print("announce: %s", ns.Transports.Announce.Status())
  ns:Print("webhook: %s", WebhookStatus())
  ns:Print("realtime: %s", ns.Transports.Screenshot.Status())
  ns:Print("time played in events: %s", ns:GetOption("general", "played") and "on" or "off")
  ns:Print("party in events: %s", ns:GetOption("general", "group") and "on" or "off")
  ns:Print("recorded for Discord (/doink enable <type>):")
  for _, notifier in ipairs(SortedNotifiers()) do
    local state = ns:GetOption(notifier.type, "enabled")
      and "|cff60ff60on|r" or "|cffff6060off|r"
    if #notifier.events == 0 then state = state .. " (stub)" end
    ns:Print("  %s: %s", notifier.type, state)
  end
end

commands.test = function(args)
  local query, count = args:match("^(%S*)%s*(%d*)$")
  if not query or query == "" then
    ns:Print("usage: /doink test <type> [count]. Types: %s", TypeList())
    return
  end
  local notifier = FindOrComplain(query)
  if not notifier then return end
  if not notifier.Test then
    ns:Print("%s has no test fixture yet", notifier.type)
    return
  end

  -- Goes through the real Emit: a disabled type isn't recorded, but its
  -- chat line is still whispered to you.
  local emitted, dropped = 0, false
  for _ = 1, tonumber(count) or 1 do
    local envelope, queued = ns:Emit(notifier.type, notifier.Test(), true)
    if queued then emitted = emitted + 1 end
    if not envelope then dropped = true end -- before login; Emit said why
  end
  if dropped then
    ns:Print("%s test dropped", notifier.type)
  elseif emitted == 0 then
    ns:Print("%s is disabled, so the test was not recorded for Discord (/doink enable %s). "
      .. "Chat announcements don't need it: with a channel set, the line was whispered to you",
      notifier.type, notifier.type)
  else
    ns:Print("emitted %d test %s event(s). /doink flush to write to disk",
      emitted, notifier.type)
  end
end

commands.dump = function(args)
  local events = ns.char.events
  if #events == 0 then
    ns:Print("queue is empty")
    return
  end
  local n = tonumber(args) or 5
  for i = math.max(1, #events - n + 1), #events do
    ns:Print("%s", events[i])
  end
end

commands.config = function()
  ns.Options.Open()
end

commands.debug = function()
  DOINKDB.debug = not DOINKDB.debug
  ns:Print("debug %s", DOINKDB.debug and "on" or "off")
end

commands.flush = function()
  ReloadUI()
end

local function SetEnabled(query, enabled)
  local notifier = FindOrComplain(query)
  if not notifier then return end
  ns:SetOption(notifier.type, "enabled", enabled)
  ns:Print("%s %s", notifier.type, enabled and "enabled" or "disabled")
end

commands.enable = function(args) SetEnabled(args, true) end
commands.disable = function(args) SetEnabled(args, false) end

local function PrintOptions(notifier)
  local overrides = ns.char.config[notifier.type] or {}
  for _, key in ipairs(SortedKeys(ns.Defaults[notifier.type] or {})) do
    local custom = overrides[key] ~= nil and " |cffffd100(custom)|r" or ""
    ns:Print("  %s %s = %s%s", notifier.type, key,
      FormatValue(key, ns:GetOption(notifier.type, key)), custom)
  end
end

commands.options = function(args)
  if args ~= "" then
    local notifier = FindOrComplain(args)
    if notifier then PrintOptions(notifier) end
    return
  end
  for _, notifier in ipairs(SortedNotifiers()) do PrintOptions(notifier) end
end

commands.set = function(args)
  local query, key, text = args:match("^(%S+)%s+(%S+)%s+(.+)$")
  if not query then
    ns:Print("usage: /doink set <type> <option> <value>. See /doink options")
    return
  end
  local notifier = FindOrComplain(query)
  if not notifier then return end

  local defaults = ns.Defaults[notifier.type] or {}
  key = key:lower()
  if defaults[key] == nil then
    ns:Print("%s has no option '%s'. Options: %s",
      notifier.type, key, table.concat(SortedKeys(defaults), ", "))
    return
  end
  local value = ParseValue(defaults[key], text)
  if value == nil then
    ns:Print("bad value '%s' for %s (%s)", text, key,
      type(defaults[key]) == "boolean" and "on/off" or "a number, or money like 1g50s")
    return
  end
  ns:SetOption(notifier.type, key, value)
  ns:Print("%s %s = %s", notifier.type, key, FormatValue(key, value))
end

commands.reset = function(args)
  local notifier = FindOrComplain(args)
  if not notifier then return end
  ns.char.config[notifier.type] = nil
  ns:Print("%s reset to defaults", notifier.type)
end

commands.webhook = function(args)
  local rest = args:match("^[Hh][Ee][Rr][Ee]%s*(.*)$") -- "here <url>"
  local here = rest ~= nil
  rest = rest or args
  local key = here and ns.player.key or "*"
  local scope = here and "this character" or "all characters"

  if rest == "" then
    ns:Print("webhook: %s", WebhookStatus())
    ns:Print("usage: /doink webhook [here] <url>|clear")
  elseif rest:lower() == "clear" then
    DOINKDB.webhooks[key] = nil
    ns:Print("webhook cleared for %s. /doink flush to apply", scope)
  elseif IsWebhook(rest) then
    DOINKDB.webhooks[key] = rest
    ns:Print("webhook set for %s. /doink flush to hand it to the companion", scope)
  else
    ns:Print("that doesn't look like a Discord webhook URL "
      .. "(https://discord.com/api/webhooks/...)")
  end
end

local function AnnounceUsage()
  ns:Print("usage: /doink announce guild|officer|party|raid|off, "
    .. "/doink announce <type> <rule>, /doink announce test")
  ns:Print("rules: level_up milestones|all|off, loot any|uncommon|rare|epic|legendary|off, "
    .. "skill_up max|milestones|all|off, death|quest|boss_kill|mount|duel|battleground on|off")
end

commands.announce = function(args)
  local announce = ns.Transports.Announce
  local sub, rest = args:match("^(%S*)%s*(.-)$")
  sub = sub:lower()

  if sub == "" then
    ns:Print("announce: %s", announce.Status())
    AnnounceUsage()
  elseif sub == "test" then
    announce.Test()
    ns:Print("whispering you a sample of each announcement")
  elseif ParseValue("", sub, ns.Choices.announce.channel) then
    ns:SetOption("announce", "channel", sub)
    ns:Print("announcements %s", sub == "off" and "off" or ("go to " .. sub .. " chat"))
  else
    local notifier = FindNotifier(sub)
    if not notifier then
      AnnounceUsage()
      return
    end
    local default = ns.Defaults.announce[notifier.type]
    local value = ParseValue(default, rest, ns.Choices.announce[notifier.type])
    if value == nil then
      AnnounceUsage()
      return
    end
    ns:SetOption("announce", notifier.type, value)
    ns:Print("announce %s: %s", notifier.type, FormatValue(notifier.type, value))
  end
end

commands.played = function(args)
  local value = ParseValue(false, args)
  if value == nil then
    ns:Print("time played in events: %s", ns:GetOption("general", "played") and "on" or "off")
    ns:Print("usage: /doink played on|off")
    return
  end
  ns:SetPlayed(value)
  if value then
    ns:Print("time played on: every recorded event carries your total time played. "
      .. "Discord shows it on level-ups")
  else
    ns:Print("time played off")
  end
end

commands.group = function(args)
  local value = ParseValue(false, args)
  if value == nil then
    ns:Print("party in events: %s", ns:GetOption("general", "group") and "on" or "off")
    ns:Print("usage: /doink group on|off")
    return
  end
  ns:SetOption("general", "group", value)
  if value then
    ns:Print("party in events on: every recorded event lists who else is in your party "
      .. "(not shown on Discord; for tools reading forward_url)")
  else
    ns:Print("party in events off")
  end
end

commands.realtime = function(args)
  local shot = ns.Transports.Screenshot
  local sub = (args:match("^(%S*)") or ""):lower()

  if sub == "on" or sub == "off" then
    shot.SetEnabled(sub == "on")
    if sub == "on" then
      ns:Print("realtime on: each event takes a screenshot that the DOINK companion "
        .. "(v0.14+) reads and deletes. You'll see \"Screen captured\" each time.")
    else
      ns:Print("realtime off")
    end
  elseif sub == "test" then
    if not DOINKDB.realtime.enabled then
      ns:Print("realtime is off. /doink realtime on first")
      return
    end
    shot.Test()
    ns:Print("taking a test screenshot; the companion should say \"Realtime works\" "
      .. "within a few seconds")
  elseif sub == "image" then
    local word = args:match("^%S+%s+(%S+)")
    local value = word and ParseValue(false, word)
    if value == nil then
      ns:Print("realtime image: %s", FormatValue("image", DOINKDB.realtime.image))
      if word then ns:Print("usage: /doink realtime image on|off") end
      return
    end
    shot.SetImage(value)
    if value then
      ns:Print("realtime image on: the companion (v0.17+) attaches each screenshot to the "
        .. "Discord post. It shows everything on your screen, chat included.")
    else
      ns:Print("realtime image off")
    end
  elseif sub == "position" or sub == "block" then
    ns:Print("no longer needed: realtime uses screenshots now")
  else
    ns:Print("realtime: %s", shot.Status())
    ns:Print("usage: /doink realtime on|off|test|image on|off")
  end
end

SLASH_DOINK1 = "/doink"
SlashCmdList.DOINK = function(msg)
  local cmd, args = msg:match("^%s*(%S*)%s*(.-)%s*$")
  local command = commands[cmd:lower()] or commands.help
  command(args)
end
