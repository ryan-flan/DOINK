local ADDON, ns = ...

-- "Your skill in %s has increased to %d." Guarded: if Forever renamed the
-- global, this notifier goes quiet instead of erroring at load.
local PATTERN = SKILL_RANK_UP and ns.FormatToPattern(SKILL_RANK_UP)

-- Max rank from the skills panel. Skills under collapsed headers aren't
-- listed, so this can return nil.
local function MaxRank(skillName)
  for i = 1, GetNumSkillLines() do
    -- name, isHeader, isExpanded, rank, numTempPoints, modifier, maxRank, ...
    local name, isHeader, _, _, _, _, maxRank = GetSkillLineInfo(i)
    if not isHeader and name == skillName then
      return maxRank
    end
  end
end

ns.Notifiers.SkillUp = {
  type = "skill_up",
  events = { "CHAT_MSG_SKILL" },

  OnEvent = function(event, msg)
    if not PATTERN then return end
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
