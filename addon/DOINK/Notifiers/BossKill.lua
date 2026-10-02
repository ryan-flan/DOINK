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
    ns:Emit("boss_kill", {
      encounter_id = encounterID,
      name = name,
      instance = (GetInstanceInfo()) or ns.Json.null,
      difficulty = difficulty,
      success = true,
      group_size = groupSize,
    })
  end,

  Test = function()
    return {
      encounter_id = 672,
      name = "Ragnaros",
      instance = "Molten Core",
      difficulty = 9,
      success = true,
      group_size = 40,
    }
  end,
}
