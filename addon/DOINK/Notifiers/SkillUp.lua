local ADDON, ns = ...

-- "Your skill in %s has increased to %d." Guarded: if Forever renamed the
-- global, this notifier goes quiet instead of erroring at load.
local PATTERN = SKILL_RANK_UP and ns.FormatToPattern(SKILL_RANK_UP)

-- Max rank, best effort. Classic clients have the skills panel API
-- (GetNumSkillLines/GetSkillLineInfo); the modern engine, which Forever
-- runs on, removed it (first two bug reports, 2026-10-03: "attempt to call
-- a nil value" at a milestone) and lists professions through
-- GetProfessions/GetProfessionInfo instead. Weapon skills aren't in that
-- list, and skills under collapsed headers aren't in the old one, so nil is
-- a normal answer. Never lets an API difference take the notifier down.
local function SkillPanelMax(skillName)
  for i = 1, GetNumSkillLines() do
    -- name, isHeader, isExpanded, rank, numTempPoints, modifier, maxRank, ...
    local name, isHeader, _, _, _, _, maxRank = GetSkillLineInfo(i)
    if not isHeader and name == skillName then
      return maxRank
    end
  end
end

local function ProfessionMax(skillName)
  for _, index in ipairs({ GetProfessions() }) do
    -- name, icon, skillLevel, maxSkillLevel, ...
    local name, _, _, maxRank = GetProfessionInfo(index)
    if name == skillName then
      return maxRank
    end
  end
end

local function MaxRank(skillName)
  local ok, maxRank
  if GetNumSkillLines and GetSkillLineInfo then
    ok, maxRank = pcall(SkillPanelMax, skillName)
  elseif GetProfessions and GetProfessionInfo then
    ok, maxRank = pcall(ProfessionMax, skillName)
  end
  if ok and type(maxRank) == "number" and maxRank > 0 then
    return maxRank
  end
end

ns.Notifiers.SkillUp = {
  type = "skill_up",
  events = { "CHAT_MSG_SKILL" },

  OnEvent = function(event, msg)
    if not PATTERN then return end
    if type(msg) ~= "string" or ns.IsSecret(msg) then
      ns:Debug("skill_up: message is secret during the encounter, skipped")
      return
    end
    local skill, rank = msg:match(PATTERN)
    rank = tonumber(rank)
    if not rank then return end

    if ns:GetOption("skill_up", "milestones_only") and rank % 75 ~= 0 then
      return
    end
    ns:Emit("skill_up", {
      skill = skill,
      rank = rank,
      max_rank = MaxRank(skill) or ns.Json.null,
    })
  end,

  Test = function()
    return { skill = "Blacksmithing", rank = 150, max_rank = 150 }
  end,
}
