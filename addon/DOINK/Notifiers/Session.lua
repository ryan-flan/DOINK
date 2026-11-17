local ADDON, ns = ...

-- Logins and logouts, for tools reading forward_url (session length, time
-- played per day). Never posted to Discord, never announced in chat, never
-- sent as a realtime screenshot: they only go in the queue, so they reach
-- the companion with the SavedVariables write at the next /reload or
-- logout. That is late, but a screenshot at every login (and a "Screen
-- captured" line) would be worse; see ns.RecordOnly in Core.lua.
--
-- PLAYER_ENTERING_WORLD(isInitialLogin, isReloadingUi) also fires on every
-- zone change between map instances; only a login or a reload is recorded.
-- isInitialLogin is "true whenever the character logs in", including
-- after logging out to character select (warcraft.wiki.gg).
--
-- PLAYER_LOGOUT is "sent when the player logs out or the UI is reloaded,
-- just before SavedVariables are saved" (warcraft.wiki.gg), so a logout
-- event written here is saved with that same write. A /reload therefore
-- records "logout" then "reload": a receiver pairs them to tell a reload
-- from a real logout. A crash or Alt-F4 saves nothing at all.
--
-- Each one also carries the character's settings at that moment, so a tool
-- can tell "nothing happened" from "not recorded" (no mount event because
-- mounts aren't ticked, say) and point the player at the right box. The
-- settings live only in SavedVariables, so this is the one way a tool sees
-- them; a change made mid-session shows at the next logout or /reload.

-- The types this character records for the companion, and the switches
-- that change what an event carries or how it travels.
local function Settings()
  local recorded = {}
  for _, notifier in pairs(ns.Notifiers) do
    if ns:GetOption(notifier.type, "enabled") then recorded[#recorded + 1] = notifier.type end
  end
  table.sort(recorded)
  local realtime = DOINKDB.realtime or {}
  return {
    recorded = recorded, -- never empty: session itself is on, or nothing is recorded
    played = ns:GetOption("general", "played") and true or false,
    group = ns:GetOption("general", "group") and true or false,
    realtime = realtime.enabled and true or false,
    realtime_image = realtime.image and true or false,
  }
end

ns.Notifiers.Session = {
  type = "session",
  events = { "PLAYER_ENTERING_WORLD", "PLAYER_LOGOUT" },

  OnEvent = function(event, isInitialLogin, isReloadingUi)
    if event == "PLAYER_LOGOUT" then
      ns:Emit("session", { state = "logout", settings = Settings() })
    elseif isInitialLogin then
      ns:Emit("session", { state = "login", settings = Settings() })
    elseif isReloadingUi then
      ns:Emit("session", { state = "reload", settings = Settings() })
    end
  end,

  Test = function()
    return {
      state = "login",
      settings = {
        recorded = { "boss_kill", "death", "level_up", "loot", "session" },
        played = true, group = false, realtime = true, realtime_image = false,
      },
    }
  end,
}
