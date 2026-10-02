local ADDON, ns = ...

-- Pixel transport (experimental). Shows each new event as a strip of black
-- and white blocks in a corner of the screen for a couple of seconds, so the
-- companion can read it off the WoW window without waiting for a /reload.
-- The companion's pixel.py mirrors this file; the layout is documented in
-- CLAUDE.md under "Pixel transport contract". Off by default: nothing here
-- creates a frame or texture until /doink realtime on.

local Pixel = {}
ns.Transports.Pixel = Pixel

local VERSION = 1
local SYNC = "1010101010110011"   -- row 0 starts with this; the reader finds it
local ROWS = 3                    -- 1 header row + 2 data rows
local MAX_BLOCKS = 400            -- strip width cap, in blocks
local MIN_BLOCKS = 104            -- sync + header must fit in row 0
local CHUNK_SECONDS = 0.25        -- how long each chunk stays on screen
local REPEATS = 4                 -- full showings per message, then it's dropped
local OUTBOX_MAX = 8
local TEST_SECONDS = 10

local CORNERS = {
  topleft = "TOPLEFT", topright = "TOPRIGHT",
  bottomleft = "BOTTOMLEFT", bottomright = "BOTTOMRIGHT",
}
Pixel.CORNERS = CORNERS
-- Windows 11 rounds a window's corners by about 8 px, and the companion's
-- screen capture sees the rounded, blended pixels, not the game's render
-- (beta, 2026-10-02: the strip's last block read as whatever the desktop
-- behind the corner was, so every chunk ending in an odd byte failed).
-- The strip keeps this many pixels clear of the side edge; the rows still
-- sit flush with the top/bottom edge, which the reader relies on.
local EDGE_INSET = 12

local band, bxor, lshift, rshift = bit.band, bit.bxor, bit.lshift, bit.rshift

------------------------------------------------------------------ encoding

-- CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF), over the chunk's payload.
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

-- One chunk as ROWS rows of 0/1, each `blocks` wide. Row 0: sync, then the
-- header (MSB first): version(4) flags(4) blocks(16) msg_id(16) index(8)
-- count(8) payload_len(8) payload_crc16(16) header_crc8(8). Rows 1+: payload
-- bytes MSB first, filling row-major. Unused blocks are 0.
local function ChunkRows(blocks, msgId, index, count, payload)
  local pc = crc16(payload)
  local header = string.char(
    lshift(VERSION, 4),                               -- version | flags
    band(rshift(blocks, 8), 0xFF), band(blocks, 0xFF),
    band(rshift(msgId, 8), 0xFF), band(msgId, 0xFF),
    index, count, #payload,
    band(rshift(pc, 8), 0xFF), band(pc, 0xFF))

  local row0 = {}
  for i = 1, #SYNC do row0[i] = tonumber(SYNC:sub(i, i)) end
  for i = 1, #header do AppendBits(row0, header:byte(i), 8) end
  AppendBits(row0, crc8(header), 8)

  local data = {}
  for i = 1, #payload do AppendBits(data, payload:byte(i), 8) end

  local rows = { row0 }
  for r = 2, ROWS do
    local row = {}
    local offset = (r - 2) * blocks
    for c = 1, blocks do row[c] = data[offset + c] or 0 end
    rows[r] = row
  end
  for c = #row0 + 1, blocks do row0[c] = 0 end
  return rows
end

function Pixel.Capacity(blocks)
  return math.floor((ROWS - 1) * blocks / 8)
end

-- Splits a message into chunks of rows. Exposed for the test harness.
function Pixel.Encode(blocks, msgId, message)
  local capacity = Pixel.Capacity(blocks)
  local count = math.max(1, math.ceil(#message / capacity))
  local chunks = {}
  for i = 0, count - 1 do
    local payload = message:sub(i * capacity + 1, (i + 1) * capacity)
    chunks[i + 1] = ChunkRows(blocks, msgId, i, count, payload)
  end
  return chunks
end

------------------------------------------------------------------ strip

local frame, blocks        -- blocks[row][col] = texture
local N, B                 -- blocks per row, pixels per block
local outbox = {}          -- { chunks = rows[], left = showings remaining }
local cursor = { msg = 1, chunk = 1 }
local current               -- rows being shown, for the harness
local acc = 0
local msgId = math.random(0, 65535) -- random start so ids don't repeat across sessions

local function Settings()
  return ns.db.realtime
end

local function Paint(rows)
  current = rows
  for r = 1, ROWS do
    local row, textures = rows[r], blocks[r]
    for c = 1, N do
      local v = row[c] or 0
      textures[c]:SetColorTexture(v, v, v, 1)
    end
  end
end

-- Shows the next chunk, or hides the strip when the outbox is empty.
local function Advance()
  if #outbox == 0 then
    if frame:IsShown() then ns:Debug("pixel: strip hidden, outbox empty") end
    frame:Hide()
    current = nil
    return
  end
  if cursor.msg > #outbox then cursor.msg, cursor.chunk = 1, 1 end
  local msg = outbox[cursor.msg]
  Paint(msg.chunks[cursor.chunk])
  frame:Show()

  cursor.chunk = cursor.chunk + 1
  if cursor.chunk > #msg.chunks then
    cursor.chunk = 1
    msg.left = msg.left - 1
    if msg.left <= 0 then
      table.remove(outbox, cursor.msg) -- the next message slides into this slot
    else
      cursor.msg = cursor.msg + 1
    end
  end
end

local function OnUpdate(_, elapsed)
  acc = acc + elapsed
  if acc < CHUNK_SECONDS then return end
  acc = 0
  Advance()
end

function Pixel.Reposition()
  if not frame then return end
  local corner = CORNERS[Settings().position] or "TOPLEFT"
  local inset = corner:find("RIGHT") and -EDGE_INSET or EDGE_INSET
  frame:ClearAllPoints()
  frame:SetPoint(corner, UIParent, corner, inset, 0)
end

local function Build()
  if frame then return end
  local physW, physH = GetPhysicalScreenSize()
  B = Settings().block or 3
  N = math.max(MIN_BLOCKS, math.min(MAX_BLOCKS, math.floor((physW - 2 * EDGE_INSET) / B)))

  -- Parented to UIParent: WorldFrame's children draw beneath every UIParent
  -- frame whatever their strata (verified in beta: a Details window's
  -- translucent backdrop dimmed the strip and corrupted it). TOOLTIP strata
  -- and a high level put it over everything else; Alt-Z hides it along with
  -- the rest of the UI, which is fine. Mouse off so it never blocks a click.
  frame = CreateFrame("Frame", nil, UIParent)
  frame:SetFrameStrata("TOOLTIP")
  frame:SetFrameLevel(10000)
  frame:EnableMouse(false)
  -- Pixel-perfect: at effective scale 1 the screen is 768 units tall, so a
  -- scale of 768/height makes one unit one physical pixel.
  if frame.SetIgnoreParentScale then
    frame:SetIgnoreParentScale(true)
    frame:SetScale(768 / physH)
  else
    frame:SetScale(768 / physH / UIParent:GetEffectiveScale())
  end
  frame:SetSize(N * B, ROWS * B)
  Pixel.Reposition()

  blocks = {}
  for r = 1, ROWS do
    blocks[r] = {}
    for c = 1, N do
      local t = frame:CreateTexture(nil, "ARTWORK")
      t:SetSize(B, B)
      t:SetPoint("TOPLEFT", frame, "TOPLEFT", (c - 1) * B, -(r - 1) * B)
      t:SetColorTexture(0, 0, 0, 1)
      blocks[r][c] = t
    end
  end
  frame:SetScript("OnUpdate", OnUpdate)
  frame:Hide()
end

local function Clear()
  outbox = {}
  cursor.msg, cursor.chunk = 1, 1
  if frame then
    frame:Hide()
    current = nil
  end
end

------------------------------------------------------------------ API

local function Queue(message, repeats)
  if not Settings().enabled then return end
  Build()
  if #outbox >= OUTBOX_MAX then table.remove(outbox, 1) end
  msgId = (msgId + 1) % 65536
  outbox[#outbox + 1] = { chunks = Pixel.Encode(N, msgId, message), left = repeats or REPEATS }
  ns:Debug("pixel: queued msg %d (%d chunks, %d bytes), strip %s", msgId,
    #outbox[#outbox].chunks, #message, frame:IsShown() and "already showing" or "starting")
  -- A hidden frame gets no OnUpdate, so start the first chunk by hand.
  if not frame:IsShown() then
    acc = 0
    Advance()
  end
end

-- Transport entry point: Core passes (json, envelope); only the JSON is
-- shown. (The envelope used to land in the repeat count. Never again.)
function Pixel.Send(message, envelope)
  Queue(message)
end

-- Tells the companion which character and addon version it's looking at.
-- A test hello stays up for ~10 s so the settings window can report it.
function Pixel.Hello(isTest)
  local GetMeta = C_AddOns and C_AddOns.GetAddOnMetadata or GetAddOnMetadata
  local hello = {
    type = "hello",
    addon = GetMeta(ADDON, "Version") or "?",
    char = ns.player.name,
    surname = ns.player.surname,
    realm = ns.player.realm,
    position = Settings().position,
  }
  if isTest then hello.test = true end
  local message = ns.Json.Encode(hello)
  local repeats
  if isTest then
    Build()
    local chunks = #Pixel.Encode(N, 0, message)
    repeats = math.ceil(TEST_SECONDS / (CHUNK_SECONDS * chunks))
  end
  Queue(message, repeats)
end

function Pixel.SetEnabled(on)
  Settings().enabled = on
  if on then
    Build()
    Pixel.Hello()
  else
    Clear()
  end
end

function Pixel.OnLogin()
  if Settings().enabled then
    Build()
    Pixel.Hello()
  end
end

function Pixel.Status()
  local s = Settings()
  if not s.enabled then return "off" end
  local shown = frame and frame:IsShown() and "strip showing" or "strip hidden"
  return ("on, %s, %s"):format(s.position or "topleft", shown)
end

-- For the offline harness only.
Pixel._test = {
  crc16 = crc16,
  crc8 = crc8,
  current = function() return current end,
  outbox = function() return outbox end,
  geometry = function() return N, B end,
  setMsgId = function(id) msgId = id end,
  advance = function(elapsed) OnUpdate(frame, elapsed) end,
  frame = function() return frame end,
}
