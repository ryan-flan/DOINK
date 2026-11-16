local ADDON, ns = ...

-- Players this character kills (killing blows only).
--
-- The combat log is protected, but the 12.0 engine Forever runs on added
-- PARTY_KILL(attackerGUID, targetGUID) (Forever 1.60.1 API docs,
-- UnitDocumentation.lua). It is tagged SecretWhenUnitIdentityRestricted:
-- secret "when the unit isn't player-controlled or in the party/raid", so
-- the GUIDs of a kill on another player should be readable; both are
-- checked anyway. Not yet seen in game: whether it fires in the open world
-- and in battlegrounds, and whether it fires for party members' kills
-- (only our own count here).
--
-- The victim's name, class, race and level come from GetPlayerInfoByGUID,
-- which may return nothing for a player the client knows nothing about;
-- those fields are null then. Its seventh value (realmName) isn't used:
-- whether Forever puts the surname there is unverified.

-- nil for a secret value or a non-string
local function Text(v)
  if ns.IsSecret(v) or type(v) ~= "string" or v == "" then return nil end
  return v
end

local function Location()
  local subzone = GetSubZoneText()
  return GetZoneText(), subzone ~= "" and subzone or ns.Json.null
end

ns.Notifiers.PvpKill = {
  type = "pvp_kill",
  events = { "PARTY_KILL" },

  OnEvent = function(event, attackerGUID, targetGUID)
    local attacker, target = Text(attackerGUID), Text(targetGUID)
    if not (attacker and target) then return end
    if not target:find("^Player%-") then return end
    local me = UnitGUID("player")
    if ns.IsSecret(me) or attacker ~= me then return end

    local class, race, name, level
    if GetPlayerInfoByGUID then
      local _, englishClass, _, englishRace, _, n, _, lvl = GetPlayerInfoByGUID(target)
      class, race, name = Text(englishClass), Text(englishRace), Text(n)
      if not ns.IsSecret(lvl) and type(lvl) == "number" and lvl > 0 then level = lvl end
    end
    local zone, subzone = Location()
    ns:Emit("pvp_kill", {
      victim = name or ns.Json.null,
      class = class or ns.Json.null,
      race = race or ns.Json.null,
      level = level or ns.Json.null,
      zone = zone,
      subzone = subzone,
    })
  end,

  Test = function()
    local zone, subzone = Location()
    return { victim = "Grakkor", class = "SHAMAN", race = "Orc", level = 24, zone = zone, subzone = subzone }
  end,
}
