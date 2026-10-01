local ADDON, ns = ...

ns.Notifiers.LevelUp = {
  type = "level_up",
  events = { "PLAYER_LEVEL_UP" },

  -- PLAYER_LEVEL_UP args: newLevel, then health/power/stat deltas.
  -- TODO(beta): check whether UnitLevel("player") is already the new level
  -- when this fires. If it lags, the envelope "level" will be one behind
  -- data.level. Check with /doink debug on a real ding.
  OnEvent = function(event, level)
    if ns:GetOption("level_up", "milestones_only") and level % 10 ~= 0 then
      return
    end
    ns:Emit("level_up", { level = level })
  end,

  Test = function()
    return { level = UnitLevel("player") + 1 }
  end,
}
