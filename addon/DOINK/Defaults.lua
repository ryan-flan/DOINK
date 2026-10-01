local ADDON, ns = ...

-- Default options per notifier type. Per-character overrides live in
-- DOINKDB.chars[key].config and only store what the player changed, so
-- editing a default here reaches existing characters. Read via ns:GetOption().
ns.Defaults = {
  level_up  = { enabled = true, milestones_only = false },
  -- Post if quality >= min_quality (3 = rare/blue) OR the stack's total
  -- vendor value >= min_vendor_value (copper; 10000 = 1g).
  loot      = { enabled = true, min_quality = 3, min_vendor_value = 10000 },
  death     = { enabled = true },
  quest     = { enabled = true },
  boss_kill = { enabled = true },
  -- milestones_only: post only at multiples of 75 (75/150/225/300).
  skill_up  = { enabled = true, milestones_only = true },
}
