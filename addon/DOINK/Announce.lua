local ADDON, ns = ...

-- In-game announcements: tells your guild (or officers, party, raid) about
-- events as they happen, in chat, with real item links. Nothing leaves the
-- game and no companion is involved. The rules are stricter than the
-- queue's (see ns.Defaults.announce) because guild chat is shared: by
-- default only level milestones, epic+ loot, boss kills and deaths.
--
-- /doink test events are whispered to you instead, so wording can be
-- checked without anyone else seeing it.

local Announce = {}
ns.Transports.Announce = Announce

local SEND_GAP = 2       -- seconds between lines
local PER_MINUTE = 8     -- beyond this, lines are dropped (and you're told)
local MAX_LEN = 255      -- chat message limit
local MAX_LEVEL = 60

local QUALITY = { any = 0, uncommon = 2, rare = 3, epic = 4, legendary = 5 }

local queue, sending, sent = {}, false, {} -- sent: GetTime() of recent lines

local function Option(key)
  return ns:GetOption("announce", key)
end

-- JSON null sentinels and empty strings count as "not there".
local function Value(v)
  if v == nil or v == ns.Json.null or v == "" then return nil end
  return v
end

local function FullName()
  local p = ns.player
  return p.surname and (p.name .. " " .. p.surname) or p.name
end

-- Chat type for the configured channel, or nil and the reason.
local function Target(channel)
  if channel == "off" then return nil, "off" end
  if channel == "guild" or channel == "officer" then
    if not IsInGuild() then return nil, "not in a guild" end
    return channel == "guild" and "GUILD" or "OFFICER"
  end
  if channel == "raid" and IsInRaid() then return "RAID" end
  if channel == "raid" or channel == "party" then
    if IsInGroup() then return "PARTY" end
    return nil, "not in a group"
  end
  return nil, "unknown channel '" .. tostring(channel) .. "'"
end

------------------------------------------------------------------ wording

-- Environmental deaths, keyed by the recap's environmentalType (upper-cased).
local ENVIRONMENT = {
  FALLING  = "Forgot I couldn't fly.",
  DROWNING = "Forgot I couldn't swim.",
  FATIGUE  = "Swam too far.",
  FIRE     = "Stood in the fire.",
  LAVA     = "Went for a swim in lava.",
  SLIME    = "Took a bath in slime.",
}

-- Reads after the chat prefix: "[Guild] Flano Wren: Ding! Level 20."
local function Describe(envelope)
  local d, t = envelope.data, envelope.type
  if t == "level_up" then
    if d.level == MAX_LEVEL then
      return ("Ding! Level %d - max level!"):format(d.level)
    end
    return ("Ding! Level %d."):format(d.level)
  elseif t == "loot" then
    local what = Value(d.link) or Value(d.name) or "something"
    if (d.qty or 1) > 1 then what = what .. "x" .. d.qty end
    return "Looted " .. what .. "!"
  elseif t == "death" then
    local where = Value(d.zone) or "parts unknown"
    if Value(d.subzone) then where = d.subzone .. ", " .. where end
    if Value(d.killer) then
      return ("Killed by %s in %s."):format(d.killer, where)
    end
    local blunder = Value(d.environment) and ENVIRONMENT[d.environment]
    if blunder then
      return ("%s Died in %s."):format(blunder, where)
    end
    return ("Died in %s."):format(where)
  elseif t == "quest" then
    return ("Completed quest: %s."):format(Value(d.title) or "?")
  elseif t == "boss_kill" then
    local line = (Value(d.name) or "Boss") .. " down!"
    local details = {}
    if Value(d.instance) then details[#details + 1] = d.instance end
    if d.group_size then details[#details + 1] = d.group_size .. " players" end
    if #details > 0 then line = line .. " (" .. table.concat(details, ", ") .. ")" end
    return line
  elseif t == "skill_up" then
    local max = Value(d.max_rank)
    if max and d.rank == max then
      return ("%s maxed at %d!"):format(d.skill, d.rank)
    end
    return ("%s %d/%s."):format(d.skill, d.rank, max or "?")
  end
end

-- Whether the per-type rule wants this event in chat.
local function Wanted(envelope)
  local d, t = envelope.data, envelope.type
  local rule = Option(t)
  if rule == nil or rule == false or rule == "off" then return false end
  if t == "level_up" then
    return rule == "all" or d.level % 10 == 0 or d.level == MAX_LEVEL
  elseif t == "loot" then
    return (d.quality or 0) >= (QUALITY[rule] or QUALITY.epic)
  elseif t == "skill_up" then
    if rule == "all" then return true end
    if rule == "max" then return Value(d.max_rank) ~= nil and d.rank == d.max_rank end
    return d.rank % 75 == 0
  end
  return rule == true
end

------------------------------------------------------------------ sending

local function Drain()
  local item = table.remove(queue, 1)
  if not item then
    sending = false
    return
  end
  local now = GetTime()
  while sent[1] and now - sent[1] > 60 do table.remove(sent, 1) end
  if #sent >= PER_MINUTE then
    ns:Print("announcement dropped (more than %d a minute): %s", PER_MINUTE, item.line)
  else
    sent[#sent + 1] = now
    SendChatMessage(item.line:sub(1, MAX_LEN), item.chatType, nil, item.target)
  end
  C_Timer.After(SEND_GAP, Drain)
end

local function Enqueue(line, chatType, target)
  queue[#queue + 1] = { line = line, chatType = chatType, target = target }
  if not sending then
    sending = true
    Drain()
  end
end

------------------------------------------------------------------ API

function Announce.Send(json, envelope)
  if not envelope or Option("channel") == "off" then return end
  local line = Describe(envelope)
  if not line then return end
  if envelope.test then
    -- Shown to you only, whatever the rules say, so you can see the wording.
    Enqueue("[test] " .. line, "WHISPER", FullName())
    return
  end
  if not Wanted(envelope) then return end
  local chatType, why = Target(Option("channel"))
  if not chatType then
    ns:Debug("announce skipped (%s): %s", why, line)
    return
  end
  Enqueue(line, chatType)
end

-- Whispers a sample line for every type to you, with whether the current
-- rules would announce it.
function Announce.Test()
  local names = {}
  for name in pairs(ns.Notifiers) do names[#names + 1] = name end
  table.sort(names)
  for _, name in ipairs(names) do
    local notifier = ns.Notifiers[name]
    if notifier.Test then
      local envelope = { type = notifier.type, data = notifier.Test() }
      local line = Describe(envelope)
      if line then
        local verdict = Wanted(envelope) and "would announce" or "would not announce"
        Enqueue(("[test] %s (%s)"):format(line, verdict), "WHISPER", FullName())
      end
    end
  end
end

function Announce.OnLogin() end

function Announce.Status()
  local channel = Option("channel")
  local chatType, why = Target(channel)
  local where = chatType and channel or (channel .. " (" .. why .. ")")
  local rules = {}
  for _, t in ipairs({ "level_up", "loot", "death", "quest", "boss_kill", "skill_up" }) do
    local rule = Option(t)
    if type(rule) == "boolean" then rule = rule and "on" or "off" end
    rules[#rules + 1] = t .. " " .. tostring(rule)
  end
  return where .. "; " .. table.concat(rules, ", ")
end

-- For the offline harness only.
Announce._test = {
  queue = function() return queue end,
  reset = function() queue, sending, sent = {}, false, {} end,
}
