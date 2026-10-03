local ADDON, ns = ...

-- The in-game settings page (Esc > Options > AddOns > DOINK, or /doink
-- config), built on the client's own Settings API: no UI library needed.
-- Every control is a proxy onto the same options the slash commands use,
-- so the two never disagree. Registered at PLAYER_LOGIN because the values
-- are per character.

local Options = {}
ns.Options = Options

local category

local VarType = Settings and Settings.VarType or { Boolean = "boolean", Number = "number", String = "string" }

local QUALITY_NAMES = {
  [0] = "Poor (grey)", [1] = "Common (white)", [2] = "Uncommon (green)",
  [3] = "Rare (blue)", [4] = "Epic (purple)", [5] = "Legendary (orange)",
}

local TYPE_NAMES = {
  level_up = "Level-ups", loot = "Loot", death = "Deaths", quest = "Quest turn-ins",
  boss_kill = "Boss kills", skill_up = "Skill-ups",
}

local ANNOUNCE_LABELS = {
  channel  = { guild = "Guild chat", officer = "Officer chat", party = "Party chat",
               raid = "Raid chat", off = "Nowhere (off)" },
  level_up = { milestones = "Levels 10, 20 ... 60", all = "Every level", off = "Never" },
  loot     = { any = "Anything", uncommon = "Uncommon (green) or better",
               rare = "Rare (blue) or better", epic = "Epic (purple) or better",
               legendary = "Legendary only", off = "Never" },
  skill_up = { max = "When a skill reaches its cap", milestones = "At 75, 150, 225, 300",
               all = "Every point", off = "Never" },
}

------------------------------------------------------------------ building blocks

local function Header(layout, title)
  if CreateSettingsListSectionHeaderInitializer then
    layout:AddInitializer(CreateSettingsListSectionHeaderInitializer(title))
  end
end

-- A proxy setting reads and writes through get/set; `variable` must be unique.
local function Proxy(variable, varType, name, default, get, set)
  return Settings.RegisterProxySetting(category, "DOINK_" .. variable, varType, name, default, get, set)
end

local function DropdownOptions(choices, labels)
  return function()
    local container = Settings.CreateControlTextContainer()
    for _, value in ipairs(choices) do
      container:Add(value, labels and labels[value] or tostring(value))
    end
    return container:GetData()
  end
end

-- Controls backed by an option (ns:GetOption / ns:SetOption). Each spec is a
-- table, so nothing depends on argument order or multiple return values.
local function OptionCheckbox(spec)
  local setting = Proxy(spec.variable, VarType.Boolean, spec.name, ns.Defaults[spec.type][spec.key],
    function() return ns:GetOption(spec.type, spec.key) end,
    function(value) ns:SetOption(spec.type, spec.key, value) end)
  Settings.CreateCheckbox(category, setting, spec.tooltip)
end

local function OptionDropdown(spec)
  local setting = Proxy(spec.variable, spec.varType, spec.name, ns.Defaults[spec.type][spec.key],
    function() return ns:GetOption(spec.type, spec.key) end,
    function(value) ns:SetOption(spec.type, spec.key, value) end)
  Settings.CreateDropdown(category, setting, DropdownOptions(spec.choices, spec.labels), spec.tooltip)
end

-- Controls with their own get/set (realtime settings are account-wide).
local function Checkbox(spec)
  Settings.CreateCheckbox(category,
    Proxy(spec.variable, VarType.Boolean, spec.name, spec.default, spec.get, spec.set), spec.tooltip)
end

local function Dropdown(spec)
  Settings.CreateDropdown(category,
    Proxy(spec.variable, spec.varType, spec.name, spec.default, spec.get, spec.set),
    DropdownOptions(spec.choices, spec.labels), spec.tooltip)
end

local function Slider(spec)
  local options = Settings.CreateSliderOptions(spec.min, spec.max, spec.step)
  options:SetLabelFormatter(MinimalSliderWithSteppersMixin.Label.Right, spec.format)
  Settings.CreateSlider(category,
    Proxy(spec.variable, VarType.Number, spec.name, spec.default, spec.get, spec.set), options, spec.tooltip)
end

------------------------------------------------------------------ sections

local function Announcements(layout)
  Header(layout, "Announcements in chat (this character)")
  local c = ns.Choices.announce
  OptionDropdown { variable = "announce_channel", varType = VarType.String, name = "Announce in",
    type = "announce", key = "channel", choices = c.channel, labels = ANNOUNCE_LABELS.channel,
    tooltip = "Where announcements go. Nothing is sent if you're not in that guild or group." }
  OptionDropdown { variable = "announce_level_up", varType = VarType.String, name = "Level-ups",
    type = "announce", key = "level_up", choices = c.level_up, labels = ANNOUNCE_LABELS.level_up }
  OptionDropdown { variable = "announce_loot", varType = VarType.String, name = "Loot",
    type = "announce", key = "loot", choices = c.loot, labels = ANNOUNCE_LABELS.loot,
    tooltip = "Minimum quality to announce. Guild chat is shared, so epic is the default." }
  OptionCheckbox { variable = "announce_death", name = "Deaths", type = "announce", key = "death" }
  OptionCheckbox { variable = "announce_boss_kill", name = "Boss kills", type = "announce", key = "boss_kill" }
  OptionCheckbox { variable = "announce_quest", name = "Quest turn-ins", type = "announce", key = "quest" }
  OptionDropdown { variable = "announce_skill_up", varType = VarType.String, name = "Skill-ups",
    type = "announce", key = "skill_up", choices = c.skill_up, labels = ANNOUNCE_LABELS.skill_up }
end

local function Discord(layout)
  Header(layout, "Discord, via the companion app (this character)")
  for _, t in ipairs({ "level_up", "loot", "death", "quest", "boss_kill", "skill_up" }) do
    OptionCheckbox { variable = "enabled_" .. t, name = TYPE_NAMES[t], type = t, key = "enabled",
      tooltip = "Record " .. TYPE_NAMES[t]:lower() .. " for the companion to post to Discord." }
  end
  OptionCheckbox { variable = "level_up_milestones", name = "Level-ups: milestones only",
    type = "level_up", key = "milestones_only", tooltip = "Only levels 10, 20 ... 60." }
  OptionDropdown { variable = "loot_min_quality", varType = VarType.Number, name = "Loot: minimum quality",
    type = "loot", key = "min_quality", choices = { 0, 1, 2, 3, 4, 5 }, labels = QUALITY_NAMES,
    tooltip = "Loot at or above this quality is recorded, whatever it sells for." }
  Slider { variable = "loot_min_vendor_value", name = "Loot: or worth at least (gold)",
    default = ns.Defaults.loot.min_vendor_value / 10000, min = 0, max = 100, step = 1,
    format = function(v) return v .. "g" end,
    get = function() return math.floor(ns:GetOption("loot", "min_vendor_value") / 10000 + 0.5) end,
    set = function(v) ns:SetOption("loot", "min_vendor_value", v * 10000) end,
    tooltip = "A stack worth this much to a vendor is recorded even below the minimum quality." }
  OptionCheckbox { variable = "skill_up_milestones", name = "Skill-ups: milestones only",
    type = "skill_up", key = "milestones_only", tooltip = "Only 75, 150, 225 and 300." }
end

local function Realtime(layout)
  Header(layout, "Realtime posting (experimental, all characters)")
  local pixel = ns.Transports.Pixel
  local function rt() return DOINKDB.realtime end

  Checkbox { variable = "realtime_enabled", name = "Enable realtime posting (experimental)", default = false,
    get = function() return rt().enabled end,
    set = function(on) pixel.SetEnabled(on) end,
    tooltip = "Posts events to Discord within a second instead of at the next /reload or logout. "
      .. "How it works: after an event the addon shows a thin black-and-white strip in a screen "
      .. "corner for about two seconds, and the DOINK companion reads it off the game window. "
      .. "Needs the companion running with \"Realtime posting\" ticked in its settings too. "
      .. "Same as /doink realtime on." }
  Dropdown { variable = "realtime_position", varType = VarType.String, name = "Corner", default = "topleft",
    choices = { "topleft", "topright", "bottomleft", "bottomright" },
    labels = { topleft = "Top left", topright = "Top right", bottomleft = "Bottom left", bottomright = "Bottom right" },
    get = function() return rt().position end,
    set = function(v) rt().position = v; pixel.Reposition() end,
    tooltip = "Move it if something else (the Discord overlay, say) sits in that corner." }
  Dropdown { variable = "realtime_block", varType = VarType.Number, name = "Block size (pixels)", default = 3,
    choices = { 2, 3, 4, 5, 6, 7, 8 },
    get = function() return rt().block end,
    set = function(v) rt().block = v end,
    tooltip = "Takes effect after /reload. Larger is easier to read on scaled displays." }
end

------------------------------------------------------------------ API

function Options.Register()
  if category or not (Settings and Settings.RegisterVerticalLayoutCategory) then return end
  local layout
  category, layout = Settings.RegisterVerticalLayoutCategory("DOINK")
  Announcements(layout)
  Discord(layout)
  Realtime(layout)
  Settings.RegisterAddOnCategory(category)
end

function Options.Open()
  if not category then
    ns:Print("settings page isn't available on this client; use the /doink commands")
    return
  end
  Settings.OpenToCategory(category:GetID())
end
