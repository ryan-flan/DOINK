local ADDON, ns = ...

-- Minimal JSON encoder. Encode only: the addon never needs to parse JSON.
local Json = {}
ns.Json = Json

-- Lua tables can't hold nil, so use this sentinel where JSON null is wanted:
--   { killer = killer or ns.Json.null }
Json.null = setmetatable({}, { __tostring = function() return "null" end })

local MAX_DEPTH = 16

local ESCAPES = {
  ['"'] = '\\"',
  ["\\"] = "\\\\",
  ["\b"] = "\\b",
  ["\f"] = "\\f",
  ["\n"] = "\\n",
  ["\r"] = "\\r",
  ["\t"] = "\\t",
}

local function escapeChar(c)
  return ESCAPES[c] or string.format("\\u%04x", c:byte())
end

-- Escapes quotes, backslashes and control characters. UTF-8 and the "|"
-- in item links ("|cff...|h[Name]|h|r") are valid JSON and pass through.
local function encodeString(s)
  local escaped = s:gsub('[%c"\\]', escapeChar)
  return '"' .. escaped .. '"'
end

local function encodeNumber(n)
  if n ~= n or n == math.huge or n == -math.huge then
    return "null" -- NaN / inf have no JSON form
  end
  if n == math.floor(n) then
    -- "%.0f", not "%d": %d truncates to a C long, which can be 32-bit.
    return string.format("%.0f", n)
  end
  return string.format("%.14g", n)
end

-- A table is an array if its keys are exactly 1..n.
local function isArray(t)
  local n = 0
  for k in pairs(t) do
    if type(k) ~= "number" or k < 1 or k ~= math.floor(k) then
      return false
    end
    n = n + 1
  end
  for i = 1, n do
    if t[i] == nil then return false end
  end
  return n > 0
end

local encodeValue

local function encodeTable(t, depth)
  if depth > MAX_DEPTH then
    error("Json: table nested too deeply (cycle?)", 0)
  end

  local parts = {}
  if isArray(t) then
    for i = 1, #t do
      parts[i] = encodeValue(t[i], depth + 1)
    end
    return "[" .. table.concat(parts, ",") .. "]"
  end

  -- Sorted keys make output deterministic, which keeps dumps diffable.
  local keys = {}
  for k in pairs(t) do keys[#keys + 1] = tostring(k) end
  table.sort(keys)
  for i, k in ipairs(keys) do
    local v = t[k]
    if v == nil then v = t[tonumber(k)] end -- numeric key in a mixed table
    parts[i] = encodeString(k) .. ":" .. encodeValue(v, depth + 1)
  end
  return "{" .. table.concat(parts, ",") .. "}"
end

encodeValue = function(v, depth)
  local t = type(v)
  if v == nil or v == Json.null then
    return "null"
  elseif t == "string" then
    return encodeString(v)
  elseif t == "number" then
    return encodeNumber(v)
  elseif t == "boolean" then
    return v and "true" or "false"
  elseif t == "table" then
    return encodeTable(v, depth)
  end
  error("Json: cannot encode a " .. t, 0)
end

function Json.Encode(value)
  return encodeValue(value, 0)
end
