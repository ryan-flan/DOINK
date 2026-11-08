local ADDON, ns = ...

-- Realtime transport. For each new event it draws a thin black-and-white
-- strip along the top edge for one frame and calls Screenshot().
-- The DOINK companion finds the screenshot in the Screenshots folder, reads
-- the grid, posts the event and deletes the file. It never touches a
-- screenshot without a valid DOINK grid. The layout is in CLAUDE.md under
-- "Screenshot transport contract"; the companion's internal/shot mirrors
-- it. Off by default: nothing here creates a frame until /doink realtime on.

local Shot = {}
ns.Transports.Screenshot = Shot

local VERSION = 2
local SYNC = "1010101010110011" -- row 0 starts with this; the reader finds it
-- Blocks per row: as wide as the screen allows, so the strip stays a few
-- rows tall (a thin line is far less noticeable than a box).
local MIN_COLS, MAX_COLS = 256, 1200
local MAX_ROWS = 32             -- data rows
local MAX_PAYLOAD = 1024        -- bytes; fits MAX_ROWS rows at MIN_COLS
local FLAG_HELLO, FLAG_IMAGE = 1, 2
local QUEUE_MAX = 8
local SPACING = 1.1   -- seconds between shots: file names have 1 s resolution
local TIMEOUT = 3     -- seconds to wait for SCREENSHOT_SUCCEEDED/FAILED
local TRIES = 2       -- a failed shot is retried once

local band, bxor, lshift, rshift = bit.band, bit.bxor, bit.lshift, bit.rshift

------------------------------------------------------------------ encoding

-- CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF), over the payload.
local function crc16(bytes)
  local crc = 0xFFFF
  for i = 1, #bytes do
    crc = bxor(crc, lshift(bytes:byte(i), 8))
    for _ = 1, 8 do
      if band(crc, 0x8000) ~= 0 then
        crc = bxor(lshift(crc, 1), 0x1021)
      else
        crc = lshift(crc, 1)
      end
      crc = band(crc, 0xFFFF)
    end
  end
  return crc
end

-- CRC-8 (poly 0x07, init 0), over the header bytes.
local function crc8(bytes)
  local crc = 0
  for i = 1, #bytes do
    crc = bxor(crc, bytes:byte(i))
    for _ = 1, 8 do
      if band(crc, 0x80) ~= 0 then
        crc = band(bxor(lshift(crc, 1), 0x07), 0xFF)
      else
        crc = band(lshift(crc, 1), 0xFF)
      end
    end
  end
  return crc
end

local function AppendBits(bits, value, count)
  for i = count - 1, 0, -1 do
    bits[#bits + 1] = band(rshift(value, i), 1)
  end
end

-- The grid as rows of 0/1, cols wide: row 0 is the sync and header, rows
-- 1..R the payload. nil if the payload is too long for one screenshot.
-- Header, MSB first: version(4) flags(4) cols(16) rows(8) payload_len(16)
-- payload_crc16(16) header_crc8(8). flags: FLAG_HELLO (1), FLAG_IMAGE (2:
-- the companion may attach the screenshot to the Discord post).
function Shot.Encode(payload, flags, cols)
  if #payload > MAX_PAYLOAD then return nil end
  local dataRows = math.max(1, math.ceil(#payload * 8 / cols))
  local pc = crc16(payload)
  local header = string.char(
    lshift(VERSION, 4) + band(flags or 0, 0x0F),
    band(rshift(cols, 8), 0xFF), band(cols, 0xFF),
    dataRows,
    band(rshift(#payload, 8), 0xFF), band(#payload, 0xFF),
    band(rshift(pc, 8), 0xFF), band(pc, 0xFF))

  local row0 = {}
  for i = 1, #SYNC do row0[i] = tonumber(SYNC:sub(i, i)) end
  for i = 1, #header do AppendBits(row0, header:byte(i), 8) end
  AppendBits(row0, crc8(header), 8)
  for c = #row0 + 1, cols do row0[c] = 0 end

  local data = {}
  for i = 1, #payload do AppendBits(data, payload:byte(i), 8) end
  local rows = { row0 }
  for r = 1, dataRows do
    local row, offset = {}, (r - 1) * cols
    for c = 1, cols do row[c] = data[offset + c] or 0 end
    rows[r + 1] = row
  end
  return rows
end

------------------------------------------------------------------ cvars

local function GetCV(name)
  if C_CVar and C_CVar.GetCVar then return C_CVar.GetCVar(name) end
  return GetCVar and GetCVar(name)
end

local function SetCV(name, value)
  if C_CVar and C_CVar.SetCVar then return C_CVar.SetCVar(name, value) end
  if SetCVar then SetCVar(name, value) end
end

local function Settings() return ns.db.realtime end

-- The companion reads JPEG and PNG, and the grid needs a quality that
-- keeps its edges. Anything else is switched for the shot and put back
-- afterwards; the old values sit in the DB in case of a /reload mid-shot.
local function SwapFormat()
  local restore = {}
  local format = GetCV("screenshotFormat")
  if format and format ~= "jpeg" and format ~= "png" then
    restore.format = format
    SetCV("screenshotFormat", "jpeg")
  end
  local quality = tonumber(GetCV("screenshotQuality") or "")
  if quality and quality < 3 then
    restore.quality = tostring(quality)
    SetCV("screenshotQuality", "3")
  end
  if next(restore) then Settings().restore = restore end
end

local function RestoreFormat()
  local restore = Settings().restore
  if not restore then return end
  if restore.format then SetCV("screenshotFormat", restore.format) end
  if restore.quality then SetCV("screenshotQuality", restore.quality) end
  Settings().restore = nil
end

------------------------------------------------------------------ grid

local grid, textures, backing -- created on first use
local B                       -- physical pixels per block
local COLS                    -- blocks per row, from the screen width
local driver                  -- always-shown frame: OnUpdate + events
local queue = {}              -- { rows = ..., tries = n }
local state = "idle"          -- idle | shooting
local shotAt = 0
local lastShotTime = -math.huge
local lastResult              -- "ok" | "failed" | "blocked"

local function Build()
  if grid then return end
  local physW, physH = GetPhysicalScreenSize()
  -- 2 px blocks: the smallest that survive the game's JPEG (tested on real
  -- retail screenshots, 2026-11-02: 2 px reads even at JPEG quality 50
  -- with 4:2:0 chroma; 1 px doesn't). One block is kept free on the right
  -- for the black backing. At 1920 px wide a typical event is 4 rows:
  -- a strip 8 px tall.
  B = 2
  COLS = math.max(MIN_COLS, math.min(MAX_COLS, math.floor(physW / B) - 1))

  -- Parented to UIParent so it draws over the UI (WorldFrame's children
  -- draw beneath every UIParent frame). Pixel-perfect: at effective scale 1
  -- the screen is 768 units tall, so 768/height makes one unit one pixel.
  -- Verified in retail: a 64x64 frame built this way lands on screenshot
  -- pixels (0,0)-(63,63) exactly.
  grid = CreateFrame("Frame", nil, UIParent)
  grid:SetFrameStrata("TOOLTIP")
  grid:SetFrameLevel(10000)
  grid:EnableMouse(false)
  if grid.SetIgnoreParentScale then
    grid:SetIgnoreParentScale(true)
    grid:SetScale(768 / physH)
  else
    grid:SetScale(768 / physH / UIParent:GetEffectiveScale())
  end
  grid:SetPoint("TOPLEFT", UIParent, "TOPLEFT", 0, 0)
  grid:SetSize((COLS + 1) * B, (MAX_ROWS + 2) * B)

  -- Black behind the grid, one block bigger on the right and bottom, so
  -- JPEG ringing at the grid's edge reads as black.
  backing = grid:CreateTexture(nil, "BACKGROUND")
  backing:SetPoint("TOPLEFT", grid, "TOPLEFT", 0, 0)
  backing:SetColorTexture(0, 0, 0, 1)

  textures = {} -- rows of block textures, created as messages need them
  grid:Hide()
end

local function Row(r)
  local line = textures[r]
  if line then return line end
  line = {}
  for c = 1, COLS do
    local t = grid:CreateTexture(nil, "ARTWORK")
    t:SetSize(B, B)
    t:SetPoint("TOPLEFT", grid, "TOPLEFT", (c - 1) * B, -(r - 1) * B)
    line[c] = t
  end
  textures[r] = line
  return line
end

local function Paint(rows)
  backing:SetSize((COLS + 1) * B, (#rows + 1) * B)
  for r = 1, math.max(#rows, #textures) do
    local row, line = rows[r], Row(r)
    for c = 1, COLS do
      local t = line[c]
      if row then
        local v = row[c]
        t:SetColorTexture(v, v, v, 1)
        t:Show()
      else
        t:Hide()
      end
    end
  end
end

local function Finish(result)
  grid:Hide()
  RestoreFormat()
  lastShotTime = GetTime()
  lastResult = result
  state = "idle"
end

local function Clear()
  queue = {}
  if grid then grid:Hide() end
  if state ~= "idle" then RestoreFormat() end
  state = "idle"
end

local function OnUpdate()
  if state == "idle" then
    if #queue == 0 or GetTime() < lastShotTime + SPACING then return end
    -- Alt-Z hides UIParent and the grid with it: wait until it's back.
    if not UIParent:IsVisible() then return end
    -- The client captures the frame it draws after Screenshot() is called
    -- (retail, 2026-11-02: a square hidden in the same handler as the call
    -- was missing from the screenshot; hidden one frame later, present).
    -- So: show and shoot now, hide on the next frame. The grid is on
    -- screen for one frame.
    Paint(queue[1].rows)
    grid:Show()
    SwapFormat()
    shotAt = GetTime()
    state = "shooting"
    Screenshot()
  elseif state == "shooting" then
    if grid:IsShown() then
      grid:Hide() -- the shot's frame has been drawn
    elseif GetTime() - shotAt > TIMEOUT then
      ns:Debug("screenshot: no answer from the client in %ds; moving on", TIMEOUT)
      table.remove(queue, 1)
      Finish("failed")
    end
  end
end

local function OnEvent(_, event, ...)
  if event == "ADDON_ACTION_BLOCKED" or event == "ADDON_ACTION_FORBIDDEN" then
    if ... ~= ADDON then return end
    Settings().enabled = false
    Clear()
    lastResult = "blocked"
    ns:Print("realtime turned off: this client blocks addon screenshots. "
      .. "Events still post at /reload or logout.")
    return
  end
  if state ~= "shooting" then return end
  if event == "SCREENSHOT_SUCCEEDED" then
    table.remove(queue, 1)
    Finish("ok")
  elseif event == "SCREENSHOT_FAILED" then
    local msg = queue[1]
    msg.tries = msg.tries + 1
    ns:Debug("screenshot failed (try %d of %d)", msg.tries, TRIES)
    if msg.tries >= TRIES then table.remove(queue, 1) end
    Finish("failed")
  end
end

local DRIVER_EVENTS = {
  "SCREENSHOT_SUCCEEDED", "SCREENSHOT_FAILED", "ADDON_ACTION_BLOCKED", "ADDON_ACTION_FORBIDDEN",
}

local function Start()
  Build()
  if not driver then
    driver = CreateFrame("Frame")
    driver:SetScript("OnEvent", OnEvent)
  end
  for _, e in ipairs(DRIVER_EVENTS) do pcall(driver.RegisterEvent, driver, e) end
  driver:SetScript("OnUpdate", OnUpdate)
  driver:Show()
end

local function Stop()
  Clear()
  if driver then
    driver:SetScript("OnUpdate", nil)
    for _, e in ipairs(DRIVER_EVENTS) do driver:UnregisterEvent(e) end
  end
end

------------------------------------------------------------------ API

local function Queue(payload, flags)
  if not Settings().enabled then return false end
  if #payload > MAX_PAYLOAD then
    ns:Debug("screenshot: message too long (%d bytes, max %d); it posts at /reload instead",
      #payload, MAX_PAYLOAD)
    return false
  end
  Start() -- sets COLS from the screen width
  local rows = Shot.Encode(payload, flags, COLS)
  if #queue >= QUEUE_MAX then
    -- Never drop the one being shot.
    table.remove(queue, state == "idle" and 1 or 2)
  end
  queue[#queue + 1] = { rows = rows, tries = 0 }
  return true
end

-- Transport entry point: Core passes (json, envelope); the JSON is sent.
function Shot.Send(json, envelope)
  Queue(json, Settings().image and FLAG_IMAGE or 0)
end

-- /doink realtime test: one screenshot the companion answers with a
-- "Realtime works" notification and deletes; nothing reaches Discord.
function Shot.Test()
  local GetMeta = C_AddOns and C_AddOns.GetAddOnMetadata or GetAddOnMetadata
  return Queue(ns.Json.Encode({
    type = "hello",
    addon = GetMeta(ADDON, "Version") or "?",
    char = ns.player.name,
    surname = ns.player.surname,
    realm = ns.player.realm,
    test = true,
  }), FLAG_HELLO)
end

-- Whether the companion may attach the screenshot to the Discord post
-- (flag bit 1 on each event's grid). Off by default: the shot shows the
-- whole screen, chat frames included.
function Shot.SetImage(on)
  Settings().image = on and true or false
end

function Shot.SetEnabled(on)
  Settings().enabled = on
  if on then Start() else Stop() end
end

-- No hello at login: it would cost a screenshot and a "Screen captured"
-- every time anyone logs in.
function Shot.OnLogin()
  RestoreFormat()
  if Settings().enabled then Start() end
end

function Shot.Status()
  if not Settings().enabled then
    return lastResult == "blocked" and "off (blocked by the client)" or "off"
  end
  local text = ("on, %d queued, image %s"):format(#queue, Settings().image and "on" or "off")
  if lastResult then text = text .. ", last shot " .. lastResult end
  return text
end

-- For the offline harness only.
Shot._test = {
  crc16 = crc16,
  crc8 = crc8,
  queue = function() return queue end,
  state = function() return state end,
  grid = function() return grid end,
  driver = function() return driver end,
  update = function() OnUpdate() end,
  textures = function() return textures end,
  block = function() return B end,
  cols = function() return COLS end,
}
