local ADDON, ns = ...

-- PLAYER_DEAD doesn't say what killed you, so watch the combat log for the
-- last thing that damaged the player and blame it if it was recent.
local KILLER_WINDOW = 5 -- seconds

local lastAttacker, lastHitAt

local function OnCombatLog()
  -- Common prefix: timestamp, subevent, hideCaster, sourceGUID, sourceName,
  -- sourceFlags, sourceRaidFlags, destGUID, destName, destFlags,
  -- destRaidFlags, then subevent-specific args.
  local _, subevent, _, _, sourceName, _, _, destGUID, _, _, _, envType =
    CombatLogGetCurrentEventInfo()
  if destGUID ~= UnitGUID("player") then return end

  if subevent == "ENVIRONMENTAL_DAMAGE" then
    lastAttacker = envType -- "Falling", "Drowning", "Lava", "Fire", ...
  elseif subevent:find("_DAMAGE$") then
    lastAttacker = sourceName
  else
    return
  end
  lastHitAt = GetTime()
end

ns.Notifiers.Death = {
  type = "death",
  events = { "PLAYER_DEAD", "COMBAT_LOG_EVENT_UNFILTERED" },

  OnEvent = function(event)
    if event == "COMBAT_LOG_EVENT_UNFILTERED" then
      OnCombatLog()
      return
    end

    -- TODO(beta): confirm PLAYER_DEAD doesn't also fire on login/reload
    -- while already dead (it would post a duplicate death).
    local killer
    if lastHitAt and GetTime() - lastHitAt <= KILLER_WINDOW then
      killer = lastAttacker
    end
    local subzone = GetSubZoneText()
    ns:Emit("death", {
      zone = GetZoneText(),
      subzone = subzone ~= "" and subzone or ns.Json.null,
      killer = killer or ns.Json.null,
    })
    lastAttacker, lastHitAt = nil, nil
  end,

  Test = function()
    local subzone = GetSubZoneText()
    return {
      zone = GetZoneText(),
      subzone = subzone ~= "" and subzone or ns.Json.null,
      killer = "Hogger",
    }
  end,
}
