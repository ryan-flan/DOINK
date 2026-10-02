local ADDON, ns = ...

-- PLAYER_DEAD doesn't say what killed you, and the combat log is protected
-- in the Forever client (see CLAUDE.md beta facts). What Forever does have is
-- retail's death recap: DeathRecap_GetEvents() returns the last hits before
-- a death, each with the attacker's name (verified 2026-10-02: ten
-- "Clattering Scorpid" entries after a real death). Environmental deaths
-- (falling, drowning, lava...) carry environmentalType instead of a source.

local RECAP_RETRY = 0.5 -- seconds; one retry if the recap is still empty at PLAYER_DEAD

local function Location()
  local subzone = GetSubZoneText()
  return GetZoneText(), subzone ~= "" and subzone or ns.Json.null
end

-- The killing blow is the newest entry: by timestamp when the client stamps
-- them, otherwise the last one listed. Returns killer, environment: one of
-- them at most. environment is the recap's environmentalType upper-cased
-- (FALLING, DROWNING, FATIGUE, FIRE, LAVA, SLIME), so chat and Discord can
-- word those deaths themselves.
local function Cause()
  if not (DeathRecap_HasEvents and DeathRecap_GetEvents) then return nil end
  if not DeathRecap_HasEvents() then return nil end
  local events = DeathRecap_GetEvents()
  if type(events) ~= "table" or #events == 0 then return nil end
  local last = events[#events]
  for _, e in ipairs(events) do
    if e.timestamp and (not last.timestamp or e.timestamp > last.timestamp) then last = e end
  end
  if last.environmentalType then return nil, string.upper(tostring(last.environmentalType)) end
  if last.sourceName and last.sourceName ~= "" then return last.sourceName end
  return nil
end

local function EmitDeath(killer, environment)
  local zone, subzone = Location()
  ns:Emit("death", { zone = zone, subzone = subzone, killer = killer or ns.Json.null,
                     environment = environment or ns.Json.null })
end

ns.Notifiers.Death = {
  type = "death",
  events = { "PLAYER_DEAD" },

  OnEvent = function(event)
    local killer, environment = Cause()
    if killer or environment or not (DeathRecap_HasEvents and C_Timer) then
      return EmitDeath(killer, environment)
    end
    -- The recap may land a moment after PLAYER_DEAD; give it one chance.
    C_Timer.After(RECAP_RETRY, function() EmitDeath(Cause()) end)
  end,

  Test = function()
    local zone, subzone = Location()
    return { zone = zone, subzone = subzone, killer = "Hogger", environment = ns.Json.null }
  end,
}
