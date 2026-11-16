local ADDON, ns = ...

-- Verified in a Forever dungeon (Shadowfang Keep, 2026-10-02):
-- ENCOUNTER_START 2748 "Rethilgore" 1 5, then ENCOUNTER_END 2748
-- "Rethilgore" 1 5 1 <table>. The sixth argument is new in Forever and
-- ignored here.

ns.Notifiers.BossKill = {
  type = "boss_kill",
  events = { "ENCOUNTER_END" },

  -- ENCOUNTER_END args: encounterID, encounterName, difficultyID,
  -- groupSize, success (1 = kill, 0 = wipe), and on Forever a trailing table
  OnEvent = function(event, encounterID, name, difficulty, groupSize, success)
    if success ~= 1 and success ~= true then return end

    ns.char.kills = ns.char.kills or {}
    local kills = ns.char.kills
    local id = tostring(encounterID)
    local first = kills[id] == nil
    -- A repeat kill isn't recorded for Discord with first_kill_only; chat
    -- still hears about it if its own rule says so.
    local filtered = ns:GetOption("boss_kill", "first_kill_only") and not first
    kills[id] = kills[id] or time()

    ns:Emit("boss_kill", {
      encounter_id = encounterID,
      name = name,
      instance = (GetInstanceInfo()) or ns.Json.null,
      difficulty = difficulty,
      success = true,
      group_size = groupSize,
      first_kill = first,
      group = ns:PartyList() or ns.Json.null,
    }, false, filtered)
  end,

  Test = function()
    return {
      encounter_id = 672,
      name = "Ragnaros",
      instance = "Molten Core",
      difficulty = 9,
      success = true,
      group_size = 40,
      first_kill = true,
      group = { "Grakkor Highmountain (Shaman 60)" },
    }
  end,
}
