local ADDON, ns = ...

-- PLAYER_DEAD doesn't say what killed you, and the combat log is protected
-- in the Forever client (see CLAUDE.md beta facts). What Forever does have is
-- retail's death recap: C_DeathRecap.GetRecapEvents() returns the last hits
-- before a death, each with the attacker's name (verified 2026-10-02 through
-- the DeathRecap_GetEvents alias: ten "Clattering Scorpid" entries after a
-- real death). Environmental deaths (falling, drowning, lava...) carry
-- environmentalType instead of a source.
--
-- DeathRecap_HasEvents / DeathRecap_GetEvents are aliases the client only
-- defines while the loadDeprecationFallbacks CVar is on (Forever 1.60.1 UI
-- source, Blizzard_DeprecatedCombatLog/Deprecated_CombatLog.lua), so the
-- C_DeathRecap functions come first and the aliases are the fallback.

local RECAP_RETRY = 0.5 -- seconds; one retry if the recap is still empty at PLAYER_DEAD
-- A hunter's saved file (2026-10-03) held two deaths one second apart with
-- the same killer: PLAYER_DEAD fires for feign death too on this client,
-- and a feign followed by the real death announced twice. Feign death is
-- never a death, and nobody dies twice within ten seconds.
local DEATH_COOLDOWN = 10
local lastDeath = -math.huge
local retryPending = false

local function Location()
  local subzone = GetSubZoneText()
  return GetZoneText(), subzone ~= "" and subzone or ns.Json.null
end

local function RecapAPI()
  local api = C_DeathRecap
  if api and api.HasRecapEvents and api.GetRecapEvents then
    return api.HasRecapEvents, api.GetRecapEvents
  end
  if DeathRecap_HasEvents and DeathRecap_GetEvents then
    return DeathRecap_HasEvents, DeathRecap_GetEvents
  end
end

-- The killing blow is the newest entry: by timestamp when the client stamps
-- them, otherwise the first one listed (Blizzard's own recap marks entry 1
-- as the hit that caused the death: `causedDeath = i == 1` in
-- Blizzard_DeathRecap.lua). Returns killer, environment: one of them at
-- most. environment is the recap's environmentalType upper-cased (FALLING,
-- DROWNING, FATIGUE, FIRE, LAVA, SLIME), so chat and Discord can word those
-- deaths themselves.
-- A recap field the client hides (a secret value) can't be compared or
-- encoded; it counts as unknown.
local function Known(v)
  if ns.IsSecret(v) then return nil end
  return v
end

local function Cause()
  local hasEvents, getEvents = RecapAPI()
  if not hasEvents then return nil end
  if not hasEvents() then return nil end
  local events = getEvents()
  if type(events) ~= "table" or #events == 0 then return nil end
  local last = events[1]
  for _, e in ipairs(events) do
    local ts, lastTs = Known(e.timestamp), Known(last.timestamp)
    if type(ts) == "number" and (type(lastTs) ~= "number" or ts > lastTs) then last = e end
  end
  local environment, source = Known(last.environmentalType), Known(last.sourceName)
  if environment then return nil, string.upper(tostring(environment)) end
  if type(source) == "string" and source ~= "" then return source end
  return nil
end

local function EmitDeath(killer, environment)
  retryPending = false
  lastDeath = GetTime()
  local zone, subzone = Location()
  ns:Emit("death", { zone = zone, subzone = subzone, killer = killer or ns.Json.null,
                     environment = environment or ns.Json.null })
end

ns.Notifiers.Death = {
  type = "death",
  events = { "PLAYER_DEAD" },

  OnEvent = function(event)
    if UnitIsFeignDeath and UnitIsFeignDeath("player") then
      ns:Debug("death: feign death, ignored")
      return
    end
    if retryPending or GetTime() - lastDeath < DEATH_COOLDOWN then
      ns:Debug("death: duplicate PLAYER_DEAD within %ds, ignored", DEATH_COOLDOWN)
      return
    end
    local killer, environment = Cause()
    if killer or environment or not (RecapAPI() and C_Timer) then
      return EmitDeath(killer, environment)
    end
    -- The recap may land a moment after PLAYER_DEAD; give it one chance.
    retryPending = true
    C_Timer.After(RECAP_RETRY, function() EmitDeath(Cause()) end)
  end,

  Test = function()
    local zone, subzone = Location()
    return { zone = zone, subzone = subzone, killer = "Hogger", environment = ns.Json.null }
  end,
}
