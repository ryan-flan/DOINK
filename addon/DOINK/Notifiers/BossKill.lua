local ADDON, ns = ...

-- Verified in a Forever dungeon (Shadowfang Keep, 2026-10-02):
-- ENCOUNTER_START 2748 "Rethilgore" 1 5, then ENCOUNTER_END 2748
-- "Rethilgore" 1 5 1 <table>. The sixth argument is new in Forever and
-- ignored here.

-- "WARRIOR" -> "Warrior"
local function TitleCase(word)
  return word:sub(1, 1):upper() .. word:sub(2):lower()
end

-- Party members other than you, "Name Surname (Class Level)", or nil in a
-- raid, when alone, or when the client hides any of it (secret values
-- during an encounter).
local function Group()
  if IsInRaid() then return nil end
  local list = {}
  for i = 1, 4 do
    local unit = "party" .. i
    if UnitExists(unit) then
      local name, second = UnitName(unit)
      local _, classFile = UnitClass(unit)
      local level = UnitLevel(unit)
      if ns.IsSecret(name) or ns.IsSecret(second) or ns.IsSecret(classFile)
          or ns.IsSecret(level) then
        return nil
      end
      if type(name) == "string" then
        local display = name .. (second and second ~= "" and (" " .. second) or "")
        local class = type(classFile) == "string" and TitleCase(classFile) or "?"
        list[#list + 1] = ("%s (%s %d)"):format(display, class, tonumber(level) or 0)
      end
    end
  end
  if #list == 0 then return nil end
  return list
end

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
    if ns:GetOption("boss_kill", "first_kill_only") and not first then return end
    kills[id] = kills[id] or time()

    ns:Emit("boss_kill", {
      encounter_id = encounterID,
      name = name,
      instance = (GetInstanceInfo()) or ns.Json.null,
      difficulty = difficulty,
      success = true,
      group_size = groupSize,
      first_kill = first,
      group = Group() or ns.Json.null,
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
      first_kill = true,
      group = { "Grakkor Highmountain (Shaman 60)" },
    }
  end,
}
