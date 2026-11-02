local ADDON, ns = ...

-- Battleground results. GetBattlefieldWinner() returns the winning faction
-- (0 = Horde, 1 = Alliance) once a match is over, and nil before. It is
-- polled on the events that fire around the end of a match; `recorded`
-- makes it one result per visit, reset when you leave the instance.

local recorded = false

ns.Notifiers.Battleground = {
  type = "battleground",
  events = { "UPDATE_BATTLEFIELD_STATUS", "PVP_MATCH_COMPLETE", "PLAYER_ENTERING_WORLD" },

  OnEvent = function()
    local _, kind = IsInInstance()
    if kind ~= "pvp" then
      recorded = false
      return
    end
    if recorded or type(GetBattlefieldWinner) ~= "function" then return end
    local winner = GetBattlefieldWinner()
    if winner ~= 0 and winner ~= 1 then return end

    local mine = UnitFactionGroup("player") == "Horde" and 0 or 1
    recorded = true
    local won = winner == mine
    if not won and ns:GetOption("battleground", "wins_only") then return end
    ns:Emit("battleground", {
      name = (GetInstanceInfo()) or GetZoneText(),
      won = won,
    })
  end,

  Test = function()
    return { name = "Warsong Gulch", won = true }
  end,
}
