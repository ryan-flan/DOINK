local ADDON, ns = ...

-- Default options per notifier type. Per-character overrides live in
-- DOINKDB.chars[key].config and only store what the player changed, so
-- editing a default here reaches existing characters. Read via ns:GetOption().
ns.Defaults = {
  level_up  = { enabled = true, milestones_only = false },
  -- Post if quality >= min_quality (3 = rare/blue) OR the stack's total
  -- vendor value >= min_vendor_value (copper; 10000 = 1g).
  -- rares_boe_only: a rare (quality 3) posts only if it is Bind on Equip
  -- (or the client doesn't say: an unknown bind type counts as BoE).
  -- Legendaries post whenever they are gear, never when they aren't.
  loot      = { enabled = true, min_quality = 3, min_vendor_value = 10000, rares_boe_only = true },
  death     = { enabled = true },
  quest     = { enabled = true },
  -- first_kill_only: post each boss only the first time this character kills it.
  boss_kill = { enabled = true, first_kill_only = false },
  -- milestones_only: post only at multiples of 75 (75/150/225/300).
  skill_up  = { enabled = true, milestones_only = true },
  mount     = { enabled = true },
  -- wins_only: losses aren't recorded.
  duel      = { enabled = true, wins_only = true },
  battleground = { enabled = true, wins_only = true },

  -- In-game chat announcements (Announce.lua). Stricter than the queue's
  -- rules above, because guild chat is shared.
  announce = {
    channel   = "guild",      -- guild | officer | party | raid | off
    level_up  = "milestones", -- milestones (10/20/.../60) | all | off
    loot      = "epic",       -- minimum quality: any | uncommon | rare | epic | legendary | off
    death     = true,
    quest     = false,
    boss_kill = true,
    skill_up  = "off",        -- max | milestones | all | off
    mount     = true,
    duel      = true,
    battleground = true,
  },
}

-- Allowed values for string options; /doink set and /doink announce check
-- against these.
ns.Choices = {
  announce = {
    channel  = { "guild", "officer", "party", "raid", "off" },
    level_up = { "milestones", "all", "off" },
    loot     = { "epic", "legendary", "rare", "uncommon", "any", "off" },
    skill_up = { "max", "milestones", "all", "off" },
  },
}
