local ADDON, ns = ...

local GetItemInfo = C_Item and C_Item.GetItemInfo or GetItemInfo

-- Only things *you* looted. "You receive item:" (quest rewards, vendor
-- buys) and "You create:" (crafting) are different strings and ignored.
-- The stack pattern must be tried first: the single pattern's (.+) would
-- also swallow "x2".
local PATTERNS = {
  { ns.FormatToPattern(LOOT_ITEM_SELF_MULTIPLE), true }, -- "You receive loot: %sx%d"
  { ns.FormatToPattern(LOOT_ITEM_SELF), false },         -- "You receive loot: %s"
}

-- itemID -> list of { link, qty } waiting for GET_ITEM_INFO_RECEIVED.
local pending = {}

local function ParseMessage(msg)
  for _, entry in ipairs(PATTERNS) do
    local link, qty = msg:match(entry[1])
    if link then
      return link, entry[2] and tonumber(qty) or 1
    end
  end
end

local function Handle(itemID, link, qty)
  -- GetItemInfo returns: name, link, quality, itemLevel, reqLevel, class,
  -- subclass, maxStack, equipLoc, texture, sellPrice, ...
  local name, _, quality, _, _, _, _, _, _, _, sellPrice = GetItemInfo(itemID)
  if not name then
    -- Not cached yet; finish when the client has it.
    pending[itemID] = pending[itemID] or {}
    table.insert(pending[itemID], { link, qty })
    ns:Debug("loot: item %d not cached, waiting", itemID)
    return
  end

  local vendorValue = (sellPrice or 0) * qty
  if quality < ns:GetOption("loot", "min_quality")
      and vendorValue < ns:GetOption("loot", "min_vendor_value") then
    return
  end

  ns:Emit("loot", {
    item_id = itemID,
    link = link,
    name = name,
    quality = quality,
    qty = qty,
    vendor_value = vendorValue, -- whole stack, in copper
  })
end

ns.Notifiers.Loot = {
  type = "loot",
  events = { "CHAT_MSG_LOOT", "GET_ITEM_INFO_RECEIVED" },

  OnEvent = function(event, ...)
    if event == "GET_ITEM_INFO_RECEIVED" then
      local itemID, success = ...
      local waiting = pending[itemID]
      if not waiting then return end
      pending[itemID] = nil
      if not success then return end
      for _, entry in ipairs(waiting) do
        Handle(itemID, entry[1], entry[2])
      end
      return
    end

    local link, qty = ParseMessage((...))
    if not link then return end -- someone else's loot, or not loot at all
    local itemID = tonumber(link:match("|Hitem:(%d+)"))
    if itemID then
      Handle(itemID, link, qty)
    end
  end,

  Test = function()
    return {
      item_id = 19019,
      link = "|cnIQ5:|Hitem:19019::::::::|h[Thunderfury, Blessed Blade of the Windseeker]|h|r",
      name = "Thunderfury, Blessed Blade of the Windseeker",
      quality = 5,
      qty = 1,
      vendor_value = 255355,
    }
  end,
}
