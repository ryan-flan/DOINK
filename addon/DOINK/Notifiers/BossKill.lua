local ADDON, ns = ...

-- TODO(beta): ENCOUNTER_END is unconfirmed in Forever dungeons. If it never
-- fires, /doink debug inside a dungeon will show nothing on a boss kill.
ns.Notifiers.BossKill = {
  type = "boss_kill",
  events = { "ENCOUNTER_END" },

  -- ENCOUNTER_END args: encounterID, encounterName, difficultyID,
  -- groupSize, success (1 = kill, 0 = wipe)
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
