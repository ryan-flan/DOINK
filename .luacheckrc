std = "lua51"
max_line_length = false
codes = true
self = false
unused_args = false -- OnEvent(event, ...), Send(json, envelope): fixed signatures
ignore = { "211/ADDON" } -- `local ADDON, ns = ...` is every file's header
exclude_files = { ".devbox/**", ".venv/**", "companion/**" }

-- Written by the addon.
globals = { "DOINKDB", "SLASH_DOINK1", "SlashCmdList" }

-- WoW API and client globals the addon reads (listed from luacheck's own
-- report on addon/DOINK; every name is a real API, see CLAUDE.md "Beta
-- facts"). Keep alphabetical. Anything luacheck flags that is NOT a known
-- API is a bug to fix, not a name to add here.
read_globals = {
  "bit", "C_AddOns", "C_CVar", "C_Item", "C_MountJournal", "C_QuestLog",
  "C_Timer", "CreateFrame", "CreateSettingsListSectionHeaderInitializer",
  "DeathRecap_GetEvents", "DeathRecap_HasEvents", "GetAddOnMetadata",
  "GetBattlefieldWinner", "GetCVar", "geterrorhandler", "GetInstanceInfo",
  "GetItemInfo", "GetItemInfoInstant", "GetNumSkillLines",
  "GetPhysicalScreenSize", "GetProfessionInfo", "GetProfessions",
  "GetRealmName", "GetSkillLineInfo", "GetSubZoneText", "GetTime",
  "GetTitleText", "GetZoneText", "IsInGroup", "IsInGuild", "IsInInstance",
  "IsInRaid", "issecretvalue", "LOOT_ITEM_SELF", "LOOT_ITEM_SELF_MULTIPLE",
  "MinimalSliderWithSteppersMixin", "ReloadUI", "Screenshot",
  "SendChatMessage", "SetCVar", "Settings", "SKILL_RANK_UP", "time",
  "UIParent", "UnitClass", "UnitExists", "UnitFactionGroup",
  "UnitIsFeignDeath", "UnitLevel", "UnitName",
}

files["addon/tests/**"] = {
  -- The harness defines the WoW API stubs as globals, and swaps a few
  -- (issecretvalue, DeathRecap_*) per test.
  allow_defined_top = true,
  ignore = { "131" }, -- unused global stubs are fine
  globals = {
    "DOINK_WRITE_FIXTURE", "DeathRecap_GetEvents", "DeathRecap_HasEvents",
    "GetProfessionInfo", "GetProfessions", "issecretvalue", "UnitIsFeignDeath",
  },
}
