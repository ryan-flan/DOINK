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

local function Environmental(kind)
  -- The client has localised names for these ("Falling", "Drowning", ...).
  local text = _G["ACTION_ENVIRONMENTAL_DAMAGE_" .. string.upper(tostring(kind))]
  return text or tostring(kind)
end

-- The killing blow is the newest entry: by timestamp when the client stamps
-- them, otherwise the last one listed.
local function Killer()
  if not (DeathRecap_HasEvents and DeathRecap_GetEvents) then return nil end
  if not DeathRecap_HasEvents() then return nil end
  local events = DeathRecap_GetEvents()
  if type(events) ~= "table" or #events == 0 then return nil end
  local last = events[#events]
  for _, e in ipairs(events) do
    if e.timestamp and (not last.timestamp or e.timestamp > last.timestamp) then last = e end
  end
  if last.sourceName and last.sourceName ~= "" then return last.sourceName end
  if last.environmentalType then return Environmental(last.environmentalType) end
  return nil
end

local function EmitDeath(killer)
  local zone, subzone = Location()
  ns:Emit("death", { zone = zone, subzone = subzone, killer = killer or ns.Json.null })
end

ns.Notifiers.Death = {
  type = "death",
  events = { "PLAYER_DEAD" },

  OnEvent = function(event)
    local killer = Killer()
    if killer or not (DeathRecap_HasEvents and C_Timer) then
      return EmitDeath(killer)
    end
    -- The recap may land a moment after PLAYER_DEAD; give it one chance.
    C_Timer.After(RECAP_RETRY, function() EmitDeath(Killer()) end)
  end,

  Test = function()
    local zone, subzone = Location()
    return { zone = zone, subzone = subzone, killer = "Hogger" }
  end,
}
