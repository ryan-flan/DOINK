local ADDON, ns = ...

-- By QUEST_TURNED_IN the quest may already be gone from the log, so also
-- remember the title shown on the reward screen (QUEST_COMPLETE) as a
-- fallback.
local lastTitle

local function QuestTitle(questID)
  if C_QuestLog and C_QuestLog.GetQuestInfo then
    local title = C_QuestLog.GetQuestInfo(questID)
    if title and title ~= "" then return title end
  end
  return lastTitle or ("Quest #" .. questID)
end

ns.Notifiers.Quest = {
  type = "quest",
  events = { "QUEST_COMPLETE", "QUEST_TURNED_IN" },

  OnEvent = function(event, ...)
    if event == "QUEST_COMPLETE" then
      lastTitle = GetTitleText()
      return
    end

    -- QUEST_TURNED_IN args: questID, xpReward, moneyReward
    local questID, xp = ...
    ns:Emit("quest", {
      quest_id = questID,
      title = QuestTitle(questID),
      xp = xp or 0,
    })
    lastTitle = nil
  end,

  Test = function()
    return { quest_id = 783, title = "A Threat Within", xp = 40 }
  end,
}
