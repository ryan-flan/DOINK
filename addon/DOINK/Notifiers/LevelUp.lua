local ADDON, ns = ...

ns.Notifiers.LevelUp = {
  type = "level_up",
  events = { "PLAYER_LEVEL_UP" },

  -- PLAYER_LEVEL_UP args: newLevel, then health/power/stat deltas.
  -- UnitLevel("player") still returns the old level at this point; Core
  -- handles that for the envelope via ns.knownLevel.
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
