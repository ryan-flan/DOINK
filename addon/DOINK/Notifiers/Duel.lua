local ADDON, ns = ...

-- Duel results arrive as system messages built from GlobalStrings:
-- DUEL_WINNER_KNOCKOUT "%1$s has defeated %2$s in a duel" and
-- DUEL_WINNER_RETREAT "%2$s has fled from %1$s in a duel". Argument 1 is
-- always the winner, argument 2 the loser, whatever order the text uses.
-- Mak'gora (the duel to the death) has its own strings whose global names
-- are unknown until launch, so they are found by content: any short
-- GlobalString mentioning Mak'gora with at least two format arguments.

local patterns -- built on the first message: { pattern, order, fled, makgora }

local function Add(list, fmt, fled, makgora)
  local pattern, order = ns.FormatToPatternOrdered(fmt)
  list[#list + 1] = { pattern = pattern, order = order, fled = fled, makgora = makgora }
end

local function BuildPatterns()
  local list = {}
  -- Mak'gora first: a Mak'gora line must not count as an ordinary duel.
  for _, value in pairs(_G) do
    if type(value) == "string" and #value < 200 and value:find("Mak'gora", 1, true) then
      local _, percents = value:gsub("%%", "")
      if percents >= 2 then Add(list, value, false, true) end
    end
  end
  for _, name in ipairs({ "DUEL_WINNER_KNOCKOUT", "DUEL_WINNER_RETREAT" }) do
    if type(_G[name]) == "string" then
      Add(list, _G[name], name == "DUEL_WINNER_RETREAT", false)
    end
  end
  return list
end

local function IsMe(name)
  name = name:gsub("%-.*$", "") -- "Flano Wren-Realm" -> "Flano Wren"
  local first, surname = UnitName("player")
  if name == first then return true end
  return surname ~= nil and surname ~= "" and name == first .. " " .. surname
end

ns.Notifiers.Duel = {
  type = "duel",
  events = { "CHAT_MSG_SYSTEM" },

  OnEvent = function(event, msg)
    if type(msg) ~= "string" or ns.IsSecret(msg) then return end
    patterns = patterns or BuildPatterns()
    for _, entry in ipairs(patterns) do
      local args = ns.MatchOrdered(msg, entry.pattern, entry.order)
      if args and args[1] and args[2] then
        local won, opponent
        if IsMe(args[1]) then
          won, opponent = true, args[2]
        elseif IsMe(args[2]) then
          won, opponent = false, args[1]
        else
          return -- someone else's duel
        end
        if not won and ns:GetOption("duel", "wins_only") then return end
        ns:Emit("duel", {
          opponent = opponent,
          won = won,
          fled = entry.fled,
          makgora = entry.makgora,
        })
        return
      end
    end
  end,

  Test = function()
    return { opponent = "Grakkor Highmountain", won = true, fled = false, makgora = false }
  end,
}
