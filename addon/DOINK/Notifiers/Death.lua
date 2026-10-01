local ADDON, ns = ...

-- killer is always null for now. PLAYER_DEAD doesn't say what killed you,
-- and the usual source, COMBAT_LOG_EVENT_UNFILTERED, is protected in the
-- Forever client: registering it triggers "DOINK has been blocked from an
-- action only available to the Blizzard UI". The companion's planned
-- combat-log tailer (WoWCombatLog.txt) is the place to fill it in.

local function Location()
  local subzone = GetSubZoneText()
  return GetZoneText(), subzone ~= "" and subzone or ns.Json.null
end

ns.Notifiers.Death = {
  type = "death",
  events = { "PLAYER_DEAD" },

  -- TODO(beta): confirm PLAYER_DEAD doesn't also fire on login/reload
  -- while already dead (it would post a duplicate death).
  OnEvent = function(event)
    local zone, subzone = Location()
    ns:Emit("death", { zone = zone, subzone = subzone, killer = ns.Json.null })
  end,

  Test = function()
    local zone, subzone = Location()
    return { zone = zone, subzone = subzone, killer = "Hogger" }
  end,
}
