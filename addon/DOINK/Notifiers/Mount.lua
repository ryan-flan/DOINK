local ADDON, ns = ...

-- New mounts. Two routes, because which one Forever uses is unverified
-- until launch:
--  * NEW_MOUNT_ADDED (modern mount journal) with C_MountJournal for the name;
--  * the mount item arriving in your bags: "You receive item:" (bought from
--    a vendor) or "You receive loot:" (a drop). Mount items are item class
--    15 (miscellaneous), subclass 5 (mount).
-- Both can fire for the same mount, so a second one within DEDUPE seconds
-- is dropped.

local GetItemInfoInstant = C_Item and C_Item.GetItemInfoInstant or GetItemInfoInstant

local DEDUPE = 120 -- seconds (GetTime)

local merchantOpen = false
local lastMountAt = -math.huge

-- Stack patterns first: the single pattern's (.+) would also swallow "x2".
local PATTERNS = {}
for _, name in ipairs({ "LOOT_ITEM_PUSHED_SELF_MULTIPLE", "LOOT_ITEM_PUSHED_SELF",
    "LOOT_ITEM_SELF_MULTIPLE", "LOOT_ITEM_SELF" }) do
  local fmt = _G[name]
  if type(fmt) == "string" then
    PATTERNS[#PATTERNS + 1] = ns.FormatToPattern(fmt)
  end
end

local function ItemLink(msg)
  for _, pattern in ipairs(PATTERNS) do
    local link = msg:match(pattern)
    if link then return link end
  end
end

local function IsMountItem(link)
  if not GetItemInfoInstant then return false end
  local ok, classID, subclassID = pcall(function()
    return select(6, GetItemInfoInstant(link))
  end)
  return ok and classID == 15 and subclassID == 5
end

local function Record(name, itemID, spellID, link)
  if GetTime() - lastMountAt < DEDUPE then
    ns:Debug("mount: %s within %ds of the last one, skipped", tostring(name), DEDUPE)
    return
  end
  lastMountAt = GetTime()
  local null = ns.Json.null
  ns:Emit("mount", {
    name = name or null,
    item_id = itemID or null,
    spell_id = spellID or null,
    link = link or null,
    bought = merchantOpen,
  })
end

ns.Notifiers.Mount = {
  type = "mount",
  events = { "NEW_MOUNT_ADDED", "CHAT_MSG_LOOT", "MERCHANT_SHOW", "MERCHANT_CLOSED" },

  OnEvent = function(event, ...)
    if event == "MERCHANT_SHOW" then
      merchantOpen = true
    elseif event == "MERCHANT_CLOSED" then
      merchantOpen = false
    elseif event == "CHAT_MSG_LOOT" then
      local msg = ...
      if type(msg) ~= "string" or ns.IsSecret(msg) then return end
      local link = ItemLink(msg)
      if not link or not IsMountItem(link) then return end
      Record(link:match("%[(.-)%]"), tonumber(link:match("|Hitem:(%d+)")), nil, link)
    elseif event == "NEW_MOUNT_ADDED" then
      local mountID = ...
      if not (C_MountJournal and C_MountJournal.GetMountInfoByID) then return end
      local ok, name, spellID = pcall(C_MountJournal.GetMountInfoByID, mountID)
      if ok and name then
        Record(name, nil, spellID, nil)
      end
    end
  end,

  Test = function()
    return {
      name = "Gray Wolf",
      item_id = 5656,
      spell_id = ns.Json.null,
      link = "|cnIQ3:|Hitem:5656::::::::|h[Gray Wolf]|h|r",
      bought = true,
    }
  end,
}
