local ADDON, ns = ...

-- Data contract constants (see CLAUDE.md before changing).
local DB_VERSION = 1
local MAX_EVENTS = 500

-- Notifier files (loaded after this one) register themselves here.
ns.Notifiers = {}

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

local function ArgsToString(...)
  local parts = {}
  for i = 1, select("#", ...) do
    parts[i] = tostring((select(i, ...)))
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
  -- Future migrations go here:
  -- if db.version < 2 then ... db.version = 2 end
  ns.db = db
end

-- PLAYER_LOGIN: name, realm and class are reliable from here on.
local function InitChar()
  local name = UnitName("player")
  local realm = GetRealmName()
  local key = name .. "-" .. realm

  local char = ns.db.chars[key] or {}
  char.seq = char.seq or 0
  char.config = char.config or {}
  char.events = char.events or {}
  ns.db.chars[key] = char

  ns.char = char
  ns.player = {
    key = key,
    name = name,
    realm = realm,
    class = select(2, UnitClass("player")), -- "WARRIOR", not localized
  }
end

------------------------------------------------------------------ emit

-- Builds the envelope, assigns seq, JSON-encodes and queues the event.
-- Returns the envelope, or nil if the event was dropped.
function ns:Emit(eventType, data, isTest)
  if not self.char then
    self:Print("dropped %s: emitted before PLAYER_LOGIN", eventType)
    return
  end
  if not self:GetOption(eventType, "enabled") then
    self:Debug("dropped %s: disabled", eventType)
    return
  end

  local char = self.char
  char.seq = char.seq + 1 -- never reused, even if encoding below fails

  local envelope = {
    seq = char.seq,
    ts = time(),
    char = self.player.name,
    realm = self.player.realm,
    class = self.player.class,
    level = UnitLevel("player"),
    type = eventType,
    data = data or {},
  }
  if isTest then envelope.test = true end

  local json = ns.Json.Encode(envelope)
  local events = char.events
  events[#events + 1] = json
  while #events > MAX_EVENTS do
    table.remove(events, 1)
  end

  self:Debug("emit %s", json)
  return envelope
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
  COMBAT_LOG_EVENT_UNFILTERED = true,
  GET_ITEM_INFO_RECEIVED = true, -- fires for every tooltip, not just loot
}

local function Dispatch(event, ...)
  local list = handlers[event]
  if not list then return end

  if not QUIET_EVENTS[event] then
    ns:Debug("%s: %s", event, ArgsToString(...))
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
  else
    Dispatch(event, ...)
  end
end)

frame:RegisterEvent("ADDON_LOADED")
frame:RegisterEvent("PLAYER_LOGIN")

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
  "/doink test <type> [count] - emit a fake event",
  "/doink dump [n] - print the last n queued events",
  "/doink debug - toggle verbose logging",
  "/doink flush - /reload to write SavedVariables",
  "/doink enable|disable <type>",
}

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

  -- Goes through the real Emit, so a disabled type is still dropped.
  local emitted = 0
  for _ = 1, tonumber(count) or 1 do
    if ns:Emit(notifier.type, notifier.Test(), true) then
      emitted = emitted + 1
    end
  end
  if emitted == 0 then
    ns:Print("%s test dropped (disabled?)", notifier.type)
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

SLASH_DOINK1 = "/doink"
SlashCmdList.DOINK = function(msg)
  local cmd, args = msg:match("^%s*(%S*)%s*(.-)%s*$")
  local command = commands[cmd:lower()] or commands.help
  command(args)
end
