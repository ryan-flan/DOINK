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

ns.Notifiers.Session = {
  type = "session",
  events = { "PLAYER_ENTERING_WORLD", "PLAYER_LOGOUT" },

  OnEvent = function(event, isInitialLogin, isReloadingUi)
    if event == "PLAYER_LOGOUT" then
      ns:Emit("session", { state = "logout" })
    elseif isInitialLogin then
      ns:Emit("session", { state = "login" })
    elseif isReloadingUi then
      ns:Emit("session", { state = "reload" })
    end
  end,

  Test = function()
    return { state = "login" }
  end,
}
