local ADDON, ns = ...

-- Everything is off until the player turns it on (like DinkPlugin).
-- Default options per notifier type. Per-character overrides live in
-- DOINKDB.chars[key].config and only store what the player changed, so
-- editing a default here reaches existing characters. Read via ns:GetOption().
ns.Defaults = {
  level_up  = { enabled = false, milestones_only = false },
  -- Post if quality >= min_quality (3 = rare/blue) OR the stack's total
  -- vendor value >= min_vendor_value (copper; 10000 = 1g).
  -- rares_boe_only: a rare (quality 3) posts only if it is Bind on Equip
  -- (or the client doesn't say: an unknown bind type counts as BoE).
  -- Legendaries post whenever they are gear, never when they aren't.
  loot      = { enabled = false, min_quality = 3, min_vendor_value = 10000, rares_boe_only = true },
  death     = { enabled = false },
  quest     = { enabled = false },
  -- first_kill_only: post each boss only the first time this character kills it.
  boss_kill = { enabled = false, first_kill_only = false },
  -- milestones_only: post only at multiples of 75 (75/150/225/300).
  skill_up  = { enabled = false, milestones_only = true },
  mount     = { enabled = false },
  -- wins_only: losses aren't recorded.
  duel      = { enabled = false, wins_only = true },
  battleground = { enabled = false, wins_only = true },

  -- In-game chat announcements (Announce.lua). Stricter than the queue's
  -- rules above, because guild chat is shared.
  announce = {
    channel   = "off",        -- off | guild | officer | party | raid
    level_up  = "off",        -- milestones (10/20/.../60) | all | off
    loot      = "off",        -- minimum quality: any | uncommon | rare | epic | legendary | off
    death     = false,
    quest     = false,
    boss_kill = false,
    skill_up  = "off",        -- max | milestones | all | off
    mount     = false,
    duel      = false,
    battleground = false,
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
